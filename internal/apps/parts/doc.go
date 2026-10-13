// Package parts stores document content and the history of each part.
// Draft scopes read through to applied content and keep files and versions under
// drafts/<member>/, so deleting that folder discards the whole scratch copy.
// Sharing a draft returns conflict: "Share works on the applied app".
package parts
