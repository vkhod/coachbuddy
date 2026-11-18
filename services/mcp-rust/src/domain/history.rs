use futures_util::stream::TryStreamExt;
use mongodb::bson::{doc, Document};
use mongodb::options::FindOptions;
use mongodb::Database;

// Input and output types are generated from schema/tools.schema.json (src/contract.rs).
pub use crate::contract::{HistoryQueryInput, HistoryQueryOutput, PastWarmupSummary};

/// Helper that builds the MongoDB filter document and find options.
/// Query filter targets: { "coachId": coach_id, "status": "completed" }
/// Query options sort descending by createdAt with a limit of 1..20.
pub fn build_history_filter_and_options(coach_id: &str, limit: i64) -> (Document, FindOptions) {
    let filter = doc! {
        "coachId": coach_id,
        "status": "completed"
    };

    let bounded_limit = limit.clamp(1, 20);
    let options = FindOptions::builder()
        .sort(doc! { "createdAt": -1 })
        .limit(bounded_limit)
        .build();

    (filter, options)
}

/// Pure extractor that parses a raw MongoDB `wod_sessions` BSON document into
/// a structured `PastWarmupSummary` containing game title, style, and drill names.
pub fn extract_past_warmup_from_doc(doc: &Document) -> Option<PastWarmupSummary> {
    let session_id = doc
        .get_str("sessionId")
        .or_else(|_| doc.get_str("_id"))
        .unwrap_or("")
        .to_string();

    let created_at = doc.get_str("createdAt").unwrap_or("").to_string();

    let warmup = match doc.get_document("warmup") {
        Ok(w) => w,
        Err(_) => return None,
    };

    let style = warmup.get_str("style").unwrap_or("").to_string();

    // 1. Extract game title (directly or from Phase 1 name)
    let mut game_title = warmup.get_str("gameTitle").unwrap_or("").to_string();
    if game_title.is_empty() {
        if let Ok(phases) = warmup.get_array("phases") {
            if let Some(first_phase) = phases.first().and_then(|p| p.as_document()) {
                if let Ok(name) = first_phase.get_str("name") {
                    game_title = name.to_string();
                }
            }
        }
    }
    if game_title.is_empty() {
        game_title = "Dynamic Functional Warm-up".to_string();
    }

    // 2. Extract drill names (from drillNames array or nested phases -> drills)
    let mut drill_names: Vec<String> = Vec::new();
    if let Ok(drills_arr) = warmup.get_array("drillNames") {
        for item in drills_arr {
            if let Some(name_str) = item.as_str() {
                drill_names.push(name_str.to_string());
            }
        }
    }

    if drill_names.is_empty() {
        if let Ok(phases) = warmup.get_array("phases") {
            for phase in phases {
                if let Some(phase_doc) = phase.as_document() {
                    if let Ok(drills) = phase_doc.get_array("drills") {
                        for drill in drills {
                            if let Some(drill_doc) = drill.as_document() {
                                if let Ok(drill_name) = drill_doc.get_str("name") {
                                    drill_names.push(drill_name.to_string());
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    Some(PastWarmupSummary {
        session_id,
        created_at,
        game_title,
        style,
        drill_names,
    })
}

/// Queries MongoDB `wod_sessions` for the coach's recent completed warm-ups.
/// This prevents repetitive game/drill programming across consecutive days.
/// Any MongoDB failure is returned as an error, never as an empty history.
pub async fn query_recent_coaching_history(
    db: &Database,
    input: HistoryQueryInput,
) -> Result<HistoryQueryOutput, String> {
    let collection = db.collection::<Document>("wod_sessions");

    let (filter, options) = build_history_filter_and_options(&input.coach_id, input.limit);
    let mut cursor = collection
        .find(filter, options)
        .await
        .map_err(|e| format!("MongoDB query error: {}", e))?;

    let mut recent_warmups: Vec<PastWarmupSummary> = Vec::new();

    // A read error fails the whole query: a partial list would look like
    // "the coach has little history" when the database is actually failing.
    while let Some(doc) = cursor
        .try_next()
        .await
        .map_err(|e| format!("MongoDB read error: {}", e))?
    {
        if let Some(summary) = extract_past_warmup_from_doc(&doc) {
            recent_warmups.push(summary);
        }
    }

    let count = recent_warmups.len();
    let summary = if count == 0 {
        format!(
            "No previous completed sessions found for coach {}. Fresh programming slate.",
            input.coach_id.as_str()
        )
    } else {
        let game_list: Vec<&str> = recent_warmups
            .iter()
            .map(|w| w.game_title.as_str())
            .collect();
        format!(
            "Found {} recent session(s). Recent warm-up games: {}. Prioritize new games/drills for variety.",
            count,
            game_list.join(", ")
        )
    };

    Ok(HistoryQueryOutput {
        coach_id: String::from(input.coach_id),
        recent_warmups,
        count: count as u32,
        summary,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_build_history_filter_and_options() {
        let (filter, options) = build_history_filter_and_options("coach-xyz", 5);
        assert_eq!(filter.get_str("coachId").unwrap(), "coach-xyz");
        assert_eq!(filter.get_str("status").unwrap(), "completed");
        assert_eq!(options.limit, Some(5));
        let sort_doc = options.sort.expect("Expected sort document");
        assert_eq!(sort_doc.get_i32("createdAt").unwrap(), -1);
    }

    #[test]
    fn test_extract_past_warmup_from_doc_direct_fields() {
        let sample_doc = doc! {
            "_id": "sess-001",
            "sessionId": "sess-001",
            "coachId": "coach-xyz",
            "status": "completed",
            "createdAt": "2026-10-24T10:00:00Z",
            "warmup": {
                "gameTitle": "PVC Broomstick Circle Drop & Switch",
                "style": "Broomstick Reaction Game + Joint Flow",
                "drillNames": ["Pass-Throughs", "Sotts Press", "Kang Squats"]
            }
        };

        let summary = extract_past_warmup_from_doc(&sample_doc).expect("Extraction should succeed");
        assert_eq!(summary.session_id, "sess-001");
        assert_eq!(summary.game_title, "PVC Broomstick Circle Drop & Switch");
        assert_eq!(summary.style, "Broomstick Reaction Game + Joint Flow");
        assert_eq!(summary.drill_names.len(), 3);
        assert_eq!(summary.drill_names[0], "Pass-Throughs");
    }

    #[test]
    fn test_extract_past_warmup_from_nested_phases() {
        let sample_doc = doc! {
            "_id": "sess-002",
            "coachId": "coach-xyz",
            "status": "completed",
            "createdAt": "2026-10-23T14:00:00Z",
            "warmup": {
                "style": "Partner Reaction Duel",
                "phases": [
                    {
                        "name": "Phase 1: Partner Mirror Footwork & Plank Tap Duel",
                        "durationMinutes": 4,
                        "drills": []
                    },
                    {
                        "name": "Phase 2: Joint Prep",
                        "durationMinutes": 8,
                        "drills": [
                            { "name": "Scapular Pulls" },
                            { "name": "Overhead Squat Therapy" }
                        ]
                    }
                ]
            }
        };

        let summary = extract_past_warmup_from_doc(&sample_doc).expect("Extraction should succeed");
        assert_eq!(summary.session_id, "sess-002");
        assert_eq!(
            summary.game_title,
            "Phase 1: Partner Mirror Footwork & Plank Tap Duel"
        );
        assert_eq!(
            summary.drill_names,
            vec!["Scapular Pulls", "Overhead Squat Therapy"]
        );
    }
}
