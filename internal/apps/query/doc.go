// Package query compiles QueryField tokens to a typed AST and parameterised
// SQLite predicates. An AST permission hook runs before compilation; personal
// space uses the identity hook. Cursors bind to the query and permission
// filters, and page by the sort value and id, including ties and nulls.
package query
