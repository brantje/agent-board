package postgres

import "testing"

func TestValidateTestDatabaseReset(t *testing.T) {
	tests := []struct {
		name         string
		databaseName string
		optIn        string
		wantErr      bool
	}{
		{name: "allows dedicated test database with opt-in", databaseName: testDatabaseName, optIn: "1"},
		{name: "rejects missing opt-in", databaseName: testDatabaseName, wantErr: true},
		{name: "rejects another database", databaseName: "agent_board", optIn: "1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTestDatabaseReset(tt.databaseName, tt.optIn)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateTestDatabaseReset(%q, %q) error=%v, wantErr=%v", tt.databaseName, tt.optIn, err, tt.wantErr)
			}
		})
	}
}
