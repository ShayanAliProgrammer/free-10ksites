#!/bin/bash
# build.sh — Build a fully standalone 10K Sites tracker binary.
#
# The resulting binary has ALL assets (HTML templates, CSS, JS, HTMX)
# embedded inside it via go:embed. It needs ZERO external files to run.
#
# Database: Turso (libSQL) if TURSO_DATABASE_URL is set, else local SQLite
# at ~/.10ksites/tracker.db (auto-created).
#
# Usage:
#   ./scripts/build.sh [output_path]
#
# Examples:
#   ./scripts/build.sh                    # → ./10ksites
#   ./scripts/build.sh /usr/local/bin/    # → /usr/local/bin/10ksites
#
# Then run from anywhere:
#   ./10ksites                            # listens on :3000
#   PORT=8080 ./10ksites                  # listens on :8080
#   TURSO_DATABASE_URL=libsql://... TURSO_AUTH_TOKEN=... ./10ksites
#   ADMIN_PASSWORD_HASH='$2a$...' ./10ksites   # bcrypt hash for production

set -e

export PATH="/home/z/.local/go/bin:$PATH"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_DIR"

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: Go is not installed. Install Go 1.21+ from https://go.dev/dl/"
  exit 1
fi

# Determine output path
OUTPUT="${1:-$PROJECT_DIR/10ksites.exe}"
if [[ "$OUTPUT" == */ ]]; then
  OUTPUT="${OUTPUT}10ksites"
fi

echo "Building standalone binary..."
echo "  Source:    $PROJECT_DIR"
echo "  Output:    $OUTPUT"
echo ""

# Build with embedded assets (CGO_ENABLED=0 for pure static binary)
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$OUTPUT" ./cmd/server/

echo ""
echo "✅ Build successful!"
echo ""
ls -la "$OUTPUT"
echo ""
echo "Run it from anywhere:"
echo "  $OUTPUT"
echo ""
echo "With Turso database:"
echo "  TURSO_DATABASE_URL=libsql://my-db.turso.io TURSO_AUTH_TOKEN=tok $OUTPUT"
echo ""
echo "With custom port/password:"
echo "  PORT=8080 ADMIN_PASSWORD=secret $OUTPUT"
echo ""
echo "For production (bcrypt password hash):"
echo "  # Generate hash: htpasswd -bnBC 10 '' 'mypassword' | tr -d ':\n'"
echo "  ADMIN_PASSWORD_HASH='\$2y\$10\$...' $OUTPUT"
echo ""
echo "The binary is self-contained:"
echo "  - HTML templates:  embedded (go:embed)"
echo "  - Tailwind CSS:    embedded (go:embed)"
echo "  - HTMX 2.0:        embedded (go:embed, self-hosted)"
echo "  - WebSocket client: embedded (go:embed)"
echo "  - SQLite driver:   pure Go (modernc.org/sqlite, no CGO)"
echo "  - Turso driver:    pure Go (libsql-client-go, no CGO)"
echo "  - Database:        auto-created at \$DB_PATH or ~/.10ksites/tracker.db"
