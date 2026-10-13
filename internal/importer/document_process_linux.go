package importer

// RLIMIT_AS bounds child address space before loading the native tool.
const documentMemoryLimitScript = `ulimit -S -v 262144 && ulimit -H -v 262144 && `
