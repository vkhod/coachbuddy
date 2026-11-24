use qrcode::types::Color;
use qrcode::{EcLevel, QrCode};
use std::fmt::Write;

// Input and output types are generated from schema/tools.schema.json (src/contract.rs).
pub use crate::contract::{QrGenerateInput, QrGenerateOutput};

/// Encodes `data` as an SVG QR code. Empty data, or data too long for a QR
/// code, is an error: the caller gets no placeholder image.
pub fn generate_qr_svg(input: QrGenerateInput) -> Result<QrGenerateOutput, String> {
    let payload = input.data.trim();
    if payload.is_empty() {
        return Err("data is empty; nothing to encode".to_string());
    }
    let size = input.size.clamp(128, 1024);
    let code = QrCode::with_error_correction_level(payload.as_bytes(), EcLevel::M)
        .map_err(|e| format!("cannot encode {} bytes as a QR code: {}", payload.len(), e))?;

    Ok(QrGenerateOutput {
        svg: render_matrix_to_svg(&code, size),
        data: payload.to_string(),
        size,
    })
}

fn render_matrix_to_svg(code: &QrCode, display_size: u32) -> String {
    let width = code.width();
    let colors = code.to_colors();
    // Four modules of white around the code: the minimum the QR standard asks for.
    let quiet_zone = 4usize;
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

    // Programme sheet look: ink modules (#121212) on plain white, square corners,
    // no frame. White, not the paper colour, because scanners read it best and it
    // fits any box's branding.
    format!(
        r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {view_size} {view_size}" width="{display_size}" height="{display_size}" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#FFFFFF"/><path d="{path_data}" fill="#121212"/></svg>"##,
        view_size = view_size,
        display_size = display_size,
        path_data = path_data
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generate_qr_svg_is_ink_on_white_without_a_frame() {
        let input = QrGenerateInput {
            data: "https://coachbuddy.app/s/abc123".to_string(),
            size: 300,
        };
        let output = generate_qr_svg(input).expect("valid URL encodes");
        assert_eq!(output.size, 300);
        assert!(output.svg.contains("<svg"));
        assert!(output.svg.contains("fill=\"#FFFFFF\""));
        assert!(output.svg.contains("fill=\"#121212\""));
        // The old Volt border, dark frame and rounded corners are gone.
        for old in ["#CCFF00", "#0B0F17", "stroke", "rx="] {
            assert!(
                !output.svg.contains(old),
                "unexpected {old} in {}",
                output.svg
            );
        }
    }

    #[test]
    fn test_generate_qr_svg_keeps_a_four_module_quiet_zone() {
        let output = generate_qr_svg(QrGenerateInput {
            data: "https://cocoach.vkhod.dev/s/cb-0123456789ab".to_string(),
            size: 256,
        })
        .expect("valid URL encodes");
        // No dark module starts before x = 4 cells of 10 units.
        let first = output.svg.split("d=\"M").nth(1).expect("path data");
        let x: usize = first.split(' ').next().unwrap().parse().unwrap();
        assert!(x >= 40, "first module at x={x}");
    }

    #[test]
    fn test_generate_qr_svg_clamps_size() {
        let output = generate_qr_svg(QrGenerateInput {
            data: " https://cocoach.vkhod.dev/s/cb-0123456789ab ".to_string(),
            size: 50,
        })
        .expect("valid URL encodes");
        assert_eq!(output.size, 128); // clamped to minimum 128
        assert_eq!(output.data, "https://cocoach.vkhod.dev/s/cb-0123456789ab");
    }

    #[test]
    fn test_generate_qr_svg_rejects_empty_data() {
        let err = generate_qr_svg(QrGenerateInput {
            data: "   ".to_string(),
            size: 256,
        })
        .unwrap_err();
        assert!(err.contains("empty"), "{err}");
    }

    #[test]
    fn test_generate_qr_svg_rejects_data_too_long_for_a_qr_code() {
        // Level M holds at most 2331 bytes; there is no placeholder image.
        let err = generate_qr_svg(QrGenerateInput {
            data: "x".repeat(3000),
            size: 256,
        })
        .unwrap_err();
        assert!(err.contains("3000 bytes"), "{err}");
    }
}
