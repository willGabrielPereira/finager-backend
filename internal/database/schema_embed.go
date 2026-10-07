package database

import _ "embed"

// Schema é o DDL completo (schema.sql), usado para montar dumps restauráveis via psql.
//
//go:embed schema.sql
var Schema string
