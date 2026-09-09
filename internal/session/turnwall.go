package session

// A worker with a bounded parent still needs time to open its working copy
// and report its actual checks. This is a worker deadline allowance; it never
// forces the conversation to delegate.
const taskAllowance = gitRootPatience + auditDeadline
