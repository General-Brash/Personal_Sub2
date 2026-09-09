package antigravity

// Production callers supply an explicit response limit; legacy fixtures use 8 MiB.
const fetchAvailableModelsBodyLimit int64 = 8 << 20
