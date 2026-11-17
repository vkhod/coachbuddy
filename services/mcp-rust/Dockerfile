FROM rust:1.100-slim-bookworm AS builder

WORKDIR /app
COPY Cargo.toml Cargo.lock ./

# Build dependencies against a dummy main first so they are cached between source changes.
RUN mkdir src && echo "fn main() {}" > src/main.rs \
    && cargo build --release --locked \
    && rm -rf src

COPY src ./src
RUN touch src/main.rs && cargo build --release --locked

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --no-create-home app

WORKDIR /app
COPY --from=builder /app/target/release/coachbuddy-mcp-rust /app/coachbuddy-mcp-rust

ENV PORT=8082
ENV RUST_LOG=info
USER app
EXPOSE 8082

CMD ["/app/coachbuddy-mcp-rust"]
