FROM rust:slim AS builder

WORKDIR /app
COPY Cargo.toml ./

# Create dummy main to cache dependencies
RUN mkdir src && echo "fn main() {}" > src/main.rs
RUN cargo build --release && rm -rf src

# Copy actual source code and build binary
COPY src ./src
RUN touch src/main.rs && cargo build --release

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/target/release/coachbuddy-mcp-rust /app/coachbuddy-mcp-rust

ENV PORT=8082
ENV RUST_LOG=info
EXPOSE 8082

CMD ["/app/coachbuddy-mcp-rust"]
