//! The shared examples in schema/examples are the same data every service tests against.

use crate::contract::{
    TimelineIssueType, TimelineStatus, VerifyTimelineInput, VerifyTimelineOutput,
};
use crate::domain::timeline::verify_timeline_math;
use serde_json::Value;
use std::path::{Path, PathBuf};

fn examples() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../schema/examples")
}

fn json_files(dir: PathBuf) -> Vec<PathBuf> {
    let mut files: Vec<PathBuf> = std::fs::read_dir(&dir)
        .unwrap_or_else(|e| panic!("cannot read {}: {e}", dir.display()))
        .map(|e| e.unwrap().path())
        .filter(|p| p.extension().is_some_and(|x| x == "json"))
        .collect();
    files.sort();
    files
}

fn load(path: &Path) -> Value {
    serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap()
}

/// Tool examples are {"$def": "<type>", "value": {...}}.
fn decode_tool_example(doc: Value) -> Result<(), String> {
    let def = doc["$def"].as_str().unwrap().to_string();
    let value = doc["value"].clone();
    let result = match def.as_str() {
        "VerifyTimelineInput" => serde_json::from_value::<VerifyTimelineInput>(value).map(|_| ()),
        "VerifyTimelineOutput" => serde_json::from_value::<VerifyTimelineOutput>(value).map(|_| ()),
        other => panic!("no decoder for tool type {other}"),
    };
    result.map_err(|e| e.to_string())
}

#[test]
fn tool_examples_match_their_folder() {
    let valid = json_files(examples().join("tools/valid"));
    assert!(!valid.is_empty());
    for f in valid {
        if let Err(e) = decode_tool_example(load(&f)) {
            panic!("{}: want accepted, got {e}", f.display());
        }
    }
    for f in json_files(examples().join("tools/invalid")) {
        assert!(
            decode_tool_example(load(&f)).is_err(),
            "{}: want rejected, got accepted",
            f.display()
        );
    }
}

fn plan_timeline(plan: &Value) -> VerifyTimelineInput {
    let tl = &plan["timeline"];
    serde_json::from_value(serde_json::json!({
        "totalMinutes": tl["totalMinutes"],
        "blocks": tl["blocks"],
    }))
    .expect("a plan timeline block is a valid tool block")
}

#[test]
fn valid_plan_timelines_verify_as_optimal() {
    for f in json_files(examples().join("plan/valid")) {
        let out = verify_timeline_math(plan_timeline(&load(&f)));
        assert_eq!(
            out.feasibility_status,
            TimelineStatus::Optimal,
            "{}: {:?}",
            f.display(),
            out.issues
        );
    }
}

#[test]
fn rule_timeline_gap_is_caught_here() {
    let f = examples().join("plan/invalid/rule-timeline-gap.json");
    let out = verify_timeline_math(plan_timeline(&load(&f)));
    assert!(!out.is_valid);
    assert_eq!(out.feasibility_status, TimelineStatus::Invalid);
    assert!(out
        .issues
        .iter()
        .any(|i| i.issue_type == TimelineIssueType::Gap));
}
