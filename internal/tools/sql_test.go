/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package tools

import (
	"testing"
)

func TestClassifySQLQuery_Read(t *testing.T) {
	tests := []struct {
		query string
		tier  ActionTier
	}{
		{"SELECT * FROM users", TierRead},
		{"select count(*) from orders", TierRead},
		{"SHOW TABLES", TierRead},
		{"DESCRIBE users", TierRead},
		{"DESC users", TierRead},
		{"EXPLAIN SELECT * FROM users", TierRead},
		{"  SELECT 1", TierRead},
	}

	for _, tt := range tests {
		tier := classifySQLQuery(tt.query)
		if tier != tt.tier {
			t.Errorf("classifySQLQuery(%q) = %s, want %s", tt.query, tier, tt.tier)
		}
	}
}

func TestClassifySQLQuery_DataMutation(t *testing.T) {
	tests := []string{
		"INSERT INTO users VALUES (1, 'test')",
		"UPDATE users SET name='test' WHERE id=1",
		"DELETE FROM users WHERE id=1",
		"MERGE INTO target USING source",
		"UPSERT INTO users VALUES (1, 'test')",
		"COPY users FROM '/tmp/data.csv'",
	}

	for _, query := range tests {
		tier := classifySQLQuery(query)
		if tier != TierDataMutation {
			t.Errorf("classifySQLQuery(%q) = %s, want data-mutation", query, tier)
		}
	}
}

func TestClassifySQLQuery_DestructiveMutation(t *testing.T) {
	tests := []string{
		"DROP TABLE users",
		"DROP DATABASE production",
		"TRUNCATE TABLE users",
		"CREATE TABLE new_table (id INT)",
		"ALTER TABLE users ADD COLUMN email VARCHAR(255)",
	}

	for _, query := range tests {
		tier := classifySQLQuery(query)
		if tier != TierDestructiveMutation {
			t.Errorf("classifySQLQuery(%q) = %s, want destructive-mutation", query, tier)
		}
	}
}

func TestClassifySQLQuery_ServiceMutation(t *testing.T) {
	tests := []string{
		"CREATE INDEX idx_users_email ON users(email)",
		"ANALYZE users",
		"VACUUM FULL",
		"GRANT SELECT ON users TO readonly_user",
		"SET search_path TO public",
	}

	for _, query := range tests {
		tier := classifySQLQuery(query)
		if tier != TierServiceMutation {
			t.Errorf("classifySQLQuery(%q) = %s, want service-mutation", query, tier)
		}
	}
}

func TestClassifySQLQuery_UnknownIsDestructive(t *testing.T) {
	// Unknown queries are fail-closed (treated as destructive)
	tier := classifySQLQuery("CALL some_procedure()")
	if tier != TierDestructiveMutation {
		t.Errorf("unknown query classified as %s, want destructive-mutation (fail-closed)", tier)
	}
}

func TestContainsSQLInjection(t *testing.T) {
	tests := []struct {
		query    string
		injected bool
	}{
		{"SELECT * FROM users", false},
		{"SELECT * FROM users; DROP TABLE users", true},         // multiple statements
		{"SELECT * FROM users -- comment", true},                // SQL comment
		{"SELECT * FROM users /* inline */", true},              // inline comment
		{"SELECT * FROM users WHERE name = '' OR '1'='1'", false}, // not flagged by simple check
		{"SELECT 1", false},
	}

	for _, tt := range tests {
		result := containsSQLInjection(tt.query)
		if result != tt.injected {
			t.Errorf("containsSQLInjection(%q) = %v, want %v", tt.query, result, tt.injected)
		}
	}
}

func TestSQLTool_ClassifyAction(t *testing.T) {
	tool := NewSQLTool(map[string]*SQLDatabase{
		"testdb": {Driver: "postgres", DSN: "postgres://localhost/test"},
	})

	// Read query should be allowed
	c := tool.ClassifyAction("sql.query", map[string]interface{}{
		"database": "testdb",
		"query":    "SELECT * FROM users",
	})
	if c.Blocked {
		t.Error("SELECT should not be blocked")
	}
	if c.Tier != TierRead {
		t.Errorf("SELECT tier = %s, want read", c.Tier)
	}

	// Write query should be blocked
	c = tool.ClassifyAction("sql.query", map[string]interface{}{
		"database": "testdb",
		"query":    "DELETE FROM users WHERE id=1",
	})
	if !c.Blocked {
		t.Error("DELETE should be blocked")
	}
	if c.Tier != TierDataMutation {
		t.Errorf("DELETE tier = %s, want data-mutation", c.Tier)
	}
}

func TestSQLTool_ExecuteBlocksNonRead(t *testing.T) {
	tool := NewSQLTool(map[string]*SQLDatabase{
		"testdb": {Driver: "postgres", DSN: "postgres://localhost/test"},
	})

	// This should be blocked before any connection attempt
	_, err := tool.Execute(nil, map[string]interface{}{
		"database": "testdb",
		"query":    "DROP TABLE users",
	})
	if err == nil {
		t.Fatal("expected error for DROP TABLE")
	}
	if !sqlContains(err.Error(), "BLOCKED") {
		t.Errorf("expected BLOCKED error, got: %v", err)
	}
}

func TestSQLTool_ExecuteBlocksSQLInjection(t *testing.T) {
	tool := NewSQLTool(map[string]*SQLDatabase{
		"testdb": {Driver: "postgres", DSN: "postgres://localhost/test"},
	})

	_, err := tool.Execute(nil, map[string]interface{}{
		"database": "testdb",
		"query":    "SELECT * FROM users; DROP TABLE users",
	})
	if err == nil {
		t.Fatal("expected error for SQL injection")
	}
	if !sqlContains(err.Error(), "BLOCKED") {
		t.Errorf("expected BLOCKED error, got: %v", err)
	}
}

func TestSQLTool_UnknownDatabase(t *testing.T) {
	tool := NewSQLTool(map[string]*SQLDatabase{
		"testdb": {Driver: "postgres", DSN: "postgres://localhost/test"},
	})

	_, err := tool.Execute(nil, map[string]interface{}{
		"database": "unknown",
		"query":    "SELECT 1",
	})
	if err == nil {
		t.Fatal("expected error for unknown database")
	}
	if !sqlContains(err.Error(), "unknown database") {
		t.Errorf("expected 'unknown database' error, got: %v", err)
	}
}

func TestSQLTool_Capability(t *testing.T) {
	tool := NewSQLTool(map[string]*SQLDatabase{})
	cap := tool.Capability()
	if cap.Domain != "sql" {
		t.Errorf("expected domain 'sql', got %s", cap.Domain)
	}
	if !cap.RequiresCredentials {
		t.Error("SQL tool should require credentials")
	}
	if len(cap.SupportedTiers) != 1 || cap.SupportedTiers[0] != TierRead {
		t.Error("SQL tool should only support read tier")
	}
}

func TestDefaultSQLProtectionClass(t *testing.T) {
	pc := DefaultSQLProtectionClass()
	if pc.Name != "sql-defaults" {
		t.Errorf("expected name 'sql-defaults', got %s", pc.Name)
	}
	if len(pc.Rules) < 7 {
		t.Errorf("expected at least 7 rules, got %d", len(pc.Rules))
	}
	// All rules should be blocks in the sql domain
	for _, r := range pc.Rules {
		if r.Action != ProtectionBlock {
			t.Errorf("expected all rules to be ProtectionBlock, got %v for %s", r.Action, r.Pattern)
		}
		if r.Domain != "sql" {
			t.Errorf("expected domain 'sql', got %s for %s", r.Domain, r.Pattern)
		}
	}
}

func TestTruncateQuery(t *testing.T) {
	short := "SELECT 1"
	if truncateQuery(short, 100) != short {
		t.Error("short query should not be truncated")
	}
	long := "SELECT very_long_column_name, another_one FROM some_extremely_long_table_name WHERE condition = 'value' AND other_condition = 'other_value'"
	result := truncateQuery(long, 50)
	if len(result) > 53 { // 50 + "..."
		t.Errorf("truncated query too long: %d", len(result))
	}
}

func TestSQLProtectionEngineIntegration(t *testing.T) {
	// Verify SQL protection class is included in default engine
	pe := NewProtectionEngine()

	// SQL data mutations should be blocked
	tests := []struct {
		action  string
		blocked bool
	}{
		{"DROP DATABASE production", true},
		{"DROP TABLE users", true},
		{"TRUNCATE users", true},
		{"DELETE FROM users WHERE id=1", true},
		{"INSERT INTO users VALUES (1)", true},
		{"UPDATE users SET name='test'", true},
		{"SELECT * FROM users", false}, // reads are fine
	}

	for _, tt := range tests {
		result := pe.Evaluate("sql", tt.action)
		if tt.blocked && result.Allowed {
			t.Errorf("protection engine should block %q", tt.action)
		}
		if !tt.blocked && !result.Allowed {
			t.Errorf("protection engine should allow %q (matched rule: %v)", tt.action, result.MatchedRule)
		}
	}
}

func TestSQLTool_MissingParams(t *testing.T) {
	tool := NewSQLTool(map[string]*SQLDatabase{
		"testdb": {Driver: "postgres", DSN: "postgres://localhost/test"},
	})

	// Missing database
	_, err := tool.Execute(nil, map[string]interface{}{
		"query": "SELECT 1",
	})
	if err == nil || !sqlContains(err.Error(), "database name is required") {
		t.Errorf("expected 'database name is required' error, got: %v", err)
	}

	// Missing query
	_, err = tool.Execute(nil, map[string]interface{}{
		"database": "testdb",
	})
	if err == nil || !sqlContains(err.Error(), "query is required") {
		t.Errorf("expected 'query is required' error, got: %v", err)
	}
}

func TestSQLTool_NameAndDescription(t *testing.T) {
	tool := NewSQLTool(map[string]*SQLDatabase{
		"app_db":     {Driver: "postgres", DSN: "postgres://localhost/app"},
		"metrics_db": {Driver: "postgres", DSN: "postgres://localhost/metrics"},
	})

	if tool.Name() != "sql.query" {
		t.Errorf("expected name 'sql.query', got %s", tool.Name())
	}

	desc := tool.Description()
	if !sqlContains(desc, "app_db") || !sqlContains(desc, "metrics_db") {
		t.Errorf("description should list databases, got: %s", desc)
	}
}

func TestClassifySQLQuery_CaseInsensitive(t *testing.T) {
	tests := []struct {
		query string
		tier  ActionTier
	}{
		{"select * from users", TierRead},
		{"Select Count(*) From Orders", TierRead},
		{"insert into users values (1)", TierDataMutation},
		{"Insert Into users VALUES (1)", TierDataMutation},
		{"drop table users", TierDestructiveMutation},
		{"Drop Table Users", TierDestructiveMutation},
	}

	for _, tt := range tests {
		tier := classifySQLQuery(tt.query)
		if tier != tt.tier {
			t.Errorf("classifySQLQuery(%q) = %s, want %s", tt.query, tier, tt.tier)
		}
	}
}

func sqlContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
