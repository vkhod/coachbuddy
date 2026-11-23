// Runs once when the server starts. The Node.js part is in its own module, so the
// Edge build never sees process.exit.

export async function register() {
  if (process.env.NEXT_RUNTIME === "nodejs") {
    await import("./instrumentation-node");
  }
}
