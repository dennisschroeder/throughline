package dashboard

import _ "embed"

// indexHTML is the entire prototype frontend: one self-contained page, no build step. It
// is intentionally disposable per the split decision (production-quality backend,
// prototype-quality frontend) — see internal/dashboard/static/index.html.
//
//go:embed static/index.html
var indexHTML []byte
