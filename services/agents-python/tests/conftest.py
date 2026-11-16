import os

# Settings the service requires at startup. Set before app modules are imported.
os.environ.setdefault("INTERNAL_GATEWAY_TOKEN", "test-internal-token")
