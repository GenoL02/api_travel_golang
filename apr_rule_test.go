package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizeAPRRuleAppliesSAPInitialValues(t *testing.T) {
	rule := APRRule{
		Client:    "100",
		RuleID:    "7",
		Bukrs:     " 1000 ",
		AmountMin: json.Number("125.50"),
		WBSOnly:   true,
		IsActive:  true,
	}

	if err := normalizeAPRRule(&rule); err != nil {
		t.Fatalf("normalizeAPRRule returned error: %v", err)
	}
	if rule.RuleID != "0007" {
		t.Errorf("RULE_ID = %q, want %q", rule.RuleID, "0007")
	}
	if rule.ApprLevel != "00" || rule.GradeLvlMin != "00" || rule.GradeLvlMax != "00" {
		t.Errorf("NUMC initial values were not padded: %+v", rule)
	}
	if rule.AmountMin.String() != "125.50" || rule.AmountMax.String() != "0.00" {
		t.Errorf("DEC values = %q, %q; want %q, %q", rule.AmountMin, rule.AmountMax, "125.50", "0.00")
	}
	if rule.ValidFrom != "00000000" || rule.ValidTo != "00000000" {
		t.Errorf("DATS initial values = %q, %q", rule.ValidFrom, rule.ValidTo)
	}
	if rule.Bukrs != "1000" {
		t.Errorf("BUKRS = %q, want trimmed value %q", rule.Bukrs, "1000")
	}
}

func TestApplyAPRRulePatchPreservesOmittedFields(t *testing.T) {
	rule := APRRule{
		Bukrs:    "1000",
		RuleText: "Existing rule",
		SLAHours: 24,
		IsActive: true,
	}
	fields := map[string]json.RawMessage{
		"RULE_TEXT": json.RawMessage(`"Updated rule"`),
		"SLA_HOURS": json.RawMessage(`0`),
		"IS_ACTIVE": json.RawMessage(`false`),
		"BUKRS":     json.RawMessage(`""`),
	}

	if err := applyAPRRulePatch(fields, &rule); err != nil {
		t.Fatalf("applyAPRRulePatch returned error: %v", err)
	}
	if rule.RuleText != "Updated rule" || rule.SLAHours != 0 || rule.IsActive || rule.Bukrs != "" {
		t.Errorf("explicit values were not applied: %+v", rule)
	}
	if rule.DocCat != "" || rule.ApprLevel != "" || rule.AmountMin != "" {
		t.Errorf("omitted fields were unexpectedly changed: %+v", rule)
	}
}

func TestApplyAPRRulePatchRejectsNullAndUnknownFields(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]json.RawMessage
	}{
		{name: "null field", fields: map[string]json.RawMessage{"RULE_TEXT": json.RawMessage(`null`)}},
		{name: "unknown field", fields: map[string]json.RawMessage{"NOT_A_FIELD": json.RawMessage(`"value"`)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := applyAPRRulePatch(test.fields, &APRRule{}); err == nil {
				t.Fatal("applyAPRRulePatch accepted invalid field")
			}
		})
	}
}

func TestNormalizeAPRRuleRejectsInvalidSAPValues(t *testing.T) {
	tests := []struct {
		name string
		rule APRRule
	}{
		{
			name: "invalid NUMC",
			rule: APRRule{Client: "100", RuleID: "A001"},
		},
		{
			name: "invalid decimal precision",
			rule: APRRule{Client: "100", RuleID: "0001", AmountMin: json.Number("12345678901234.00")},
		},
		{
			name: "invalid DATS date",
			rule: APRRule{Client: "100", RuleID: "0001", ValidFrom: "20260230"},
		},
		{
			name: "oversized CHAR",
			rule: APRRule{Client: "100", RuleID: "0001", RuleText: "1234567890123456789012345678901234567890123456789012345678901"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := normalizeAPRRule(&test.rule); err == nil {
				t.Fatal("normalizeAPRRule accepted invalid value")
			}
		})
	}
}

func TestNormalizeSAPDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "PostgreSQL ISO date", input: "2026-10-02", want: "2026-10-02"},
		{name: "SAP DATS date", input: "20261002", want: "2026-10-02"},
		{name: "SAP initial date", input: "00000000", want: "00000000"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeSAPDate("VALID_FROM", test.input)
			if err != nil {
				t.Fatalf("normalizeSAPDate returned error: %v", err)
			}
			if got != test.want {
				t.Errorf("normalizeSAPDate() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveCurrentDate(t *testing.T) {
	now := time.Date(2026, time.October, 2, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "defaults to current date", want: "2026-10-02"},
		{name: "accepts ISO date", input: "2026-10-01", want: "2026-10-01"},
		{name: "accepts SAP DATS", input: "20261001", want: "2026-10-01"},
		{name: "rejects SAP initial date", input: "00000000", wantErr: true},
		{name: "rejects invalid date", input: "2026-02-30", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveCurrentDate(test.input, now)
			if (err != nil) != test.wantErr {
				t.Fatalf("resolveCurrentDate() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Errorf("resolveCurrentDate() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeCodeTAppliesInitialValues(t *testing.T) {
	codeT := CodeT{
		Client:   "100",
		CodeType: " TRIP_TYPE ",
		Code:     " DOM ",
		Langu:    " E ",
		CodeText: " Domestic ",
	}

	if err := normalizeCodeT(&codeT); err != nil {
		t.Fatalf("normalizeCodeT returned error: %v", err)
	}
	if codeT.CodeType != "TRIP_TYPE" || codeT.Code != "DOM" || codeT.CodeText != "Domestic" {
		t.Errorf("text fields were not trimmed: %+v", codeT)
	}
	if codeT.SortNo != "000" || codeT.Criticality != "0" {
		t.Errorf("NUMC initial values = SORT_NO %q, CRITICALITY %q", codeT.SortNo, codeT.Criticality)
	}
}

func TestNormalizeCodeTRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		code CodeT
	}{
		{
			name: "missing key",
			code: CodeT{Client: "100", CodeType: "TYPE", Langu: "E"},
		},
		{
			name: "invalid sort number",
			code: CodeT{Client: "100", CodeType: "TYPE", Code: "A", Langu: "E", SortNo: "A01"},
		},
		{
			name: "invalid criticality",
			code: CodeT{Client: "100", CodeType: "TYPE", Code: "A", Langu: "E", Criticality: "10"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := normalizeCodeT(&test.code); err == nil {
				t.Fatal("normalizeCodeT accepted invalid fields")
			}
		})
	}
}
