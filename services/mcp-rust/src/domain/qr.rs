use qrcode::types::Color;
use qrcode::{EcLevel, QrCode};
use serde::{Deserialize, Serialize};
use std::fmt::Write;

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct QrGenerateInput {
    pub data: String,
    #[serde(default = "default_qr_size")]
    pub size: u32,
}

fn default_qr_size() -> u32 {
    256
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct QrGenerateOutput {
    pub svg: String,
    pub data: String,
    pub size: u32,
}

pub fn generate_qr_svg(input: QrGenerateInput) -> QrGenerateOutput {
    let payload = if input.data.trim().is_empty() {
        "https://coachbuddy.app".to_string()
    } else {
        input.data.trim().to_string()
    };
    let size = input.size.clamp(128, 1024);

    let svg = match QrCode::with_error_correction_level(payload.as_bytes(), EcLevel::M) {
        Ok(code) => render_matrix_to_svg(&code, size),
        Err(_) => render_fallback_svg(size),
    };

    QrGenerateOutput {
        svg,
        data: payload,
        size,
    }
}

fn render_matrix_to_svg(code: &QrCode, display_size: u32) -> String {
    let width = code.width();
    let colors = code.to_colors();
    let quiet_zone = 3usize;
    let total_modules = width + quiet_zone * 2;
    let cell = 10usize;
    let view_size = total_modules * cell;

    let mut path_data = String::with_capacity(width * width * 16);

    for y in 0..width {
        for x in 0..width {
            if colors[y * width + x] == Color::Dark {
                let px = (x + quiet_zone) * cell;
                let py = (y + quiet_zone) * cell;
                let _ = write!(path_data, "M{} {}h{}v{}h-{}z", px, py, cell, cell, cell);
            }
        }
    }

    // High-contrast dark modules (#0B0F17) on a pure white (#FFFFFF) QR canvas
    // surrounded by the signature CoachBuddy Volt (#CCFF00) accent border and dark frame (#0B0F17).
    format!(
        r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {view_size} {view_size}" width="{display_size}" height="{display_size}" shape-rendering="crispEdges"><rect width="100%" height="100%" rx="18" fill="#0B0F17"/><rect x="8" y="8" width="{inner}" height="{inner}" rx="14" fill="#FFFFFF" stroke="#CCFF00" stroke-width="4"/><path d="{path_data}" fill="#0B0F17"/></svg>"##,
        view_size = view_size,
        display_size = display_size,
        inner = view_size.saturating_sub(16),
        path_data = path_data
    )
}

fn render_fallback_svg(display_size: u32) -> String {
    format!(
        r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="{display_size}" height="{display_size}"><rect width="100%" height="100%" rx="18" fill="#0B0F17"/><rect x="8" y="8" width="240" height="240" rx="14" fill="#FFFFFF" stroke="#CCFF00" stroke-width="4"/><rect x="32" y="32" width="60" height="60" rx="4" fill="#0B0F17"/><rect x="164" y="32" width="60" height="60" rx="4" fill="#0B0F17"/><rect x="32" y="164" width="60" height="60" rx="4" fill="#0B0F17"/></svg>"##,
        display_size = display_size
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generate_qr_svg_produces_high_contrast_volt_frame() {
        let input = QrGenerateInput {
            data: "https://coachbuddy.app/s/abc123".to_string(),
            size: 300,
        };
        let output = generate_qr_svg(input);
        assert_eq!(output.size, 300);
        assert!(output.svg.contains("<svg"));
        assert!(output.svg.contains("stroke=\"#CCFF00\""));
        assert!(output.svg.contains("fill=\"#FFFFFF\""));
        assert!(output.svg.contains("fill=\"#0B0F17\""));
    }

    #[test]
    fn test_generate_qr_svg_defaults_and_clamping() {
        let output = generate_qr_svg(QrGenerateInput {
            data: "".to_string(),
            size: 50,
        });
        assert_eq!(output.size, 128); // clamped to minimum 128
        assert_eq!(output.data, "https://coachbuddy.app");
    }
}
