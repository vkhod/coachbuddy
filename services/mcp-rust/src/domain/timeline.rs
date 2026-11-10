use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct TimelineBlockInput {
    pub start_min: u32,
    pub end_min: u32,
    pub label: String,
    #[serde(default = "default_block_type", rename = "type")]
    pub block_type: String,
    #[serde(default)]
    pub notes: String,
}

fn default_block_type() -> String {
    "metcon".to_string()
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct VerifyTimelineInput {
    #[serde(default = "default_total_minutes")]
    pub total_minutes: u32,
    #[serde(default)]
    pub blocks: Vec<TimelineBlockInput>,
}

fn default_total_minutes() -> u32 {
    60
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct TimelineIssue {
    pub block_index: usize,
    pub block_label: String,
    pub issue_type: String, // "GAP", "OVERLAP", "INVALID_DURATION", "SUM_MISMATCH", "EMPTY_TIMELINE"
    pub description: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct VerifyTimelineOutput {
    pub is_valid: bool,
    pub total_minutes: u32,
    pub scheduled_minutes: u32,
    pub discrepancy_minutes: i32,
    pub feasibility_status: String, // "OPTIMAL", "TIGHT", "INVALID"
    pub coach_warnings: Vec<String>,
    pub issues: Vec<TimelineIssue>,
    pub blocks: Vec<TimelineBlockInput>,
}

/// Pure deterministic timeline math verifier.
/// Verifies that the LLM-generated minute-by-minute schedule is mathematically sound:
/// 1. Exactly contiguous (no unexplained gaps or overlaps between blocks).
/// 2. All blocks have positive non-zero duration (end_min > start_min).
/// 3. The schedule covers 0..total_minutes without truncating or overrunning.
///
/// Leaves LLM block labels, coaching cues, and notes 100% intact.
pub fn verify_timeline_math(input: VerifyTimelineInput) -> VerifyTimelineOutput {
    let total_minutes = if input.total_minutes == 0 {
        60
    } else {
        input.total_minutes
    };

    let mut issues = Vec::new();
    let mut coach_warnings = Vec::new();

    if input.blocks.is_empty() {
        issues.push(TimelineIssue {
            block_index: 0,
            block_label: "None".to_string(),
            issue_type: "EMPTY_TIMELINE".to_string(),
            description: "No timeline blocks were provided for verification.".to_string(),
        });
        return VerifyTimelineOutput {
            is_valid: false,
            total_minutes,
            scheduled_minutes: 0,
            discrepancy_minutes: total_minutes as i32,
            feasibility_status: "INVALID".to_string(),
            coach_warnings: vec!["Cannot verify an empty timeline schedule.".to_string()],
            issues,
            blocks: input.blocks,
        };
    }

    // =========================================================================
    // TODO(engineer) 1.2: Contiguity and Sum Invariant Check Loop
    //
    // Core Invariants to check across `input.blocks`:
    // 1. Maintain an `expected_cursor: u32 = 0`.
    // 2. For each block at `(idx, block)`:
    //    a) Duration: verify `block.end_min > block.start_min`.
    //       If not, record issue "INVALID_DURATION".
    //    b) Gap: If `block.start_min > expected_cursor`,
    //       record issue "GAP" with gap size `block.start_min - expected_cursor`.
    //    c) Overlap: If `block.start_min < expected_cursor`,
    //       record issue "OVERLAP" with overlap size `expected_cursor - block.start_min`.
    //    d) Advance cursor: `expected_cursor = block.end_min`.
    // 3. Final Sum check:
    //    let scheduled_minutes = expected_cursor;
    //    let discrepancy_minutes = (total_minutes as i32) - (scheduled_minutes as i32);
    //    If `scheduled_minutes != total_minutes`, record issue "SUM_MISMATCH".
    // 4. Feasibility Status:
    //    - If no issues -> "OPTIMAL"
    //    - If only a minor sum mismatch (abs(discrepancy) <= 5) and zero gaps/overlaps -> "TIGHT"
    //    - Otherwise (gaps, overlaps, negative durations, or large discrepancy) -> "INVALID"
    // =========================================================================

    let mut expected_cursor: u32 = 0;
    let mut has_gaps_or_overlaps = false;

    for (idx, block) in input.blocks.iter().enumerate() {
        // Invariant A: Duration must be strictly positive
        if block.end_min <= block.start_min {
            issues.push(TimelineIssue {
                block_index: idx,
                block_label: block.label.clone(),
                issue_type: "INVALID_DURATION".to_string(),
                description: format!(
                    "Block '{}' has invalid duration (startMin: {}, endMin: {})",
                    block.label, block.start_min, block.end_min
                ),
            });
        }

        // Invariant B: Contiguity check against expected_cursor
        if block.start_min > expected_cursor {
            has_gaps_or_overlaps = true;
            let gap = block.start_min - expected_cursor;
            issues.push(TimelineIssue {
                block_index: idx,
                block_label: block.label.clone(),
                issue_type: "GAP".to_string(),
                description: format!(
                    "Unscheduled gap of {} minute(s) before '{}' (expected start: {}, actual: {})",
                    gap, block.label, expected_cursor, block.start_min
                ),
            });
        } else if block.start_min < expected_cursor {
            has_gaps_or_overlaps = true;
            let overlap = expected_cursor - block.start_min;
            issues.push(TimelineIssue {
                block_index: idx,
                block_label: block.label.clone(),
                issue_type: "OVERLAP".to_string(),
                description: format!(
                    "Timeline collision: overlap of {} minute(s) before '{}' (expected start: {}, actual: {})",
                    overlap, block.label, expected_cursor, block.start_min
                ),
            });
        }

        expected_cursor = block.end_min;
    }

    let scheduled_minutes = expected_cursor;
    let discrepancy_minutes = (total_minutes as i32) - (scheduled_minutes as i32);

    if discrepancy_minutes != 0 {
        issues.push(TimelineIssue {
            block_index: input.blocks.len().saturating_sub(1),
            block_label: "End of Class".to_string(),
            issue_type: "SUM_MISMATCH".to_string(),
            description: format!(
                "Total scheduled minutes ({}) does not match class cap ({}). Discrepancy: {} minute(s)",
                scheduled_minutes, total_minutes, discrepancy_minutes.abs()
            ),
        });
    }

    let is_valid = issues.is_empty();
    let feasibility_status = if is_valid {
        coach_warnings.push(
            "Timeline verified: 100% contiguous 0..totalMinutes coverage with zero gaps or collisions."
                .to_string(),
        );
        "OPTIMAL".to_string()
    } else if !has_gaps_or_overlaps && discrepancy_minutes.abs() <= 5 {
        coach_warnings.push(format!(
            "TIGHT SCHEDULE: Schedule has {} minute(s) discrepancy against the {}-minute cap, but blocks are contiguous.",
            discrepancy_minutes.abs(),
            total_minutes
        ));
        "TIGHT".to_string()
    } else {
        coach_warnings.push(
            "INVALID TIMELINE: Mathematical discrepancies detected. Adjust block durations to restore contiguity."
                .to_string(),
        );
        "INVALID".to_string()
    };

    VerifyTimelineOutput {
        is_valid,
        total_minutes,
        scheduled_minutes,
        discrepancy_minutes,
        feasibility_status,
        coach_warnings,
        issues,
        blocks: input.blocks,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_valid_60m_blocks() -> Vec<TimelineBlockInput> {
        vec![
            TimelineBlockInput {
                start_min: 0,
                end_min: 5,
                label: "Whiteboard Brief".to_string(),
                block_type: "brief".to_string(),
                notes: "Brief athletes on movement stimulus".to_string(),
            },
            TimelineBlockInput {
                start_min: 5,
                end_min: 17,
                label: "Dynamic Warm-up & Game".to_string(),
                block_type: "warmup".to_string(),
                notes: "Broomstick reaction game".to_string(),
            },
            TimelineBlockInput {
                start_min: 17,
                end_min: 20,
                label: "Transition: Barbell Loading".to_string(),
                block_type: "transition".to_string(),
                notes: "3-minute station setup".to_string(),
            },
            TimelineBlockInput {
                start_min: 20,
                end_min: 35,
                label: "Strength: Front Squat E2MOM".to_string(),
                block_type: "strength".to_string(),
                notes: "Film and critique bottom depth".to_string(),
            },
            TimelineBlockInput {
                start_min: 35,
                end_min: 38,
                label: "Transition: Metcon Prep".to_string(),
                block_type: "transition".to_string(),
                notes: "3-minute warm-up sets".to_string(),
            },
            TimelineBlockInput {
                start_min: 38,
                end_min: 55,
                label: "Metcon: 15min AMRAP".to_string(),
                block_type: "metcon".to_string(),
                notes: "Coach active floor pacing".to_string(),
            },
            TimelineBlockInput {
                start_min: 55,
                end_min: 60,
                label: "Cooldown & Equipment Reset".to_string(),
                block_type: "cooldown".to_string(),
                notes: "Parasympathetic breathing".to_string(),
            },
        ]
    }

    #[test]
    fn test_verify_timeline_math_optimal() {
        let input = VerifyTimelineInput {
            total_minutes: 60,
            blocks: sample_valid_60m_blocks(),
        };
        let output = verify_timeline_math(input);
        assert!(output.is_valid);
        assert_eq!(output.feasibility_status, "OPTIMAL");
        assert_eq!(output.discrepancy_minutes, 0);
        assert_eq!(output.scheduled_minutes, 60);
        assert!(output.issues.is_empty());
        assert_eq!(output.blocks.len(), 7);
    }

    #[test]
    fn test_verify_timeline_math_detects_gap() {
        let mut blocks = sample_valid_60m_blocks();
        // Introduce a gap between 5 and 8
        blocks[1].start_min = 8;
        let input = VerifyTimelineInput {
            total_minutes: 60,
            blocks,
        };
        let output = verify_timeline_math(input);
        assert!(!output.is_valid);
        assert_eq!(output.feasibility_status, "INVALID");
        let has_gap_issue = output.issues.iter().any(|i| i.issue_type == "GAP");
        assert!(has_gap_issue, "Should identify gap issue");
    }

    #[test]
    fn test_verify_timeline_math_detects_overlap() {
        let mut blocks = sample_valid_60m_blocks();
        // Introduce an overlap: block 1 starts at 3 instead of 5
        blocks[1].start_min = 3;
        let input = VerifyTimelineInput {
            total_minutes: 60,
            blocks,
        };
        let output = verify_timeline_math(input);
        assert!(!output.is_valid);
        assert_eq!(output.feasibility_status, "INVALID");
        let has_overlap = output.issues.iter().any(|i| i.issue_type == "OVERLAP");
        assert!(has_overlap, "Should identify overlap issue");
    }

    #[test]
    fn test_verify_timeline_math_detects_sum_mismatch() {
        let mut blocks = sample_valid_60m_blocks();
        // End at 58 instead of 60 (discrepancy of 2 mins)
        blocks[6].end_min = 58;
        let input = VerifyTimelineInput {
            total_minutes: 60,
            blocks,
        };
        let output = verify_timeline_math(input);
        assert!(!output.is_valid);
        // Small discrepancy (<= 5) without gaps is classified as TIGHT
        assert_eq!(output.feasibility_status, "TIGHT");
        assert_eq!(output.discrepancy_minutes, 2);
        let has_sum = output.issues.iter().any(|i| i.issue_type == "SUM_MISMATCH");
        assert!(has_sum, "Should identify sum mismatch");
    }

    #[test]
    fn test_verify_timeline_math_detects_invalid_duration() {
        let mut blocks = sample_valid_60m_blocks();
        blocks[2].end_min = blocks[2].start_min; // 0 duration
        let input = VerifyTimelineInput {
            total_minutes: 60,
            blocks,
        };
        let output = verify_timeline_math(input);
        assert!(!output.is_valid);
        let has_dur_issue = output
            .issues
            .iter()
            .any(|i| i.issue_type == "INVALID_DURATION");
        assert!(has_dur_issue, "Should flag zero/negative duration");
    }

    #[test]
    fn test_verify_timeline_math_empty_blocks() {
        let input = VerifyTimelineInput {
            total_minutes: 60,
            blocks: vec![],
        };
        let output = verify_timeline_math(input);
        assert!(!output.is_valid);
        assert_eq!(output.feasibility_status, "INVALID");
        assert_eq!(output.issues[0].issue_type, "EMPTY_TIMELINE");
    }
}
