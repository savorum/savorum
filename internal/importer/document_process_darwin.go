package importer

// Shared system mappings prevent a useful fixed RLIMIT_AS on macOS. Native
// extraction requires host memory isolation for a hard memory boundary.
const documentMemoryLimitScript = ""
