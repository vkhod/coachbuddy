// Shrinks a screenshot in the browser before upload (plan 2.1): about 1600 px
// on the long edge, JPEG, so uploads stay small on mobile data.

const LONG_EDGE = 1600;
const QUALITY = 0.85;

export async function toUploadDataUrl(file: File): Promise<string> {
  if (!file.type.startsWith("image/")) {
    throw new Error("Pick an image file: a screenshot or a photo of the board.");
  }
  const bitmap = await createImageBitmap(file).catch(() => {
    throw new Error("This image could not be read. Try another screenshot.");
  });
  const scale = Math.min(1, LONG_EDGE / Math.max(bitmap.width, bitmap.height));
  const width = Math.round(bitmap.width * scale);
  const height = Math.round(bitmap.height * scale);
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("This browser cannot prepare the image.");
  ctx.fillStyle = "#ffffff";
  ctx.fillRect(0, 0, width, height);
  ctx.drawImage(bitmap, 0, 0, width, height);
  bitmap.close();
  return canvas.toDataURL("image/jpeg", QUALITY);
}
