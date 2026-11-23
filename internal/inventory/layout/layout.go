// Package layout names inventory storage without depending on its live observers.
package layout

// File is shared by inventory writers and the unused-home reader, which cannot
// import the live inventory package because its key storage depends on identity.
const File = "inventory.json"
