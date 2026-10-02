package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"
)

type APRRuleMax struct {
	// Client string `json:"CLIENT"`
	RuleID string `json:"RULE_ID"`
	// Bukrs  string `json:"BUKRS"`
	// DocCat       string      `json:"DOC_CAT"`
	// ApprLevel    string      `json:"APPR_LEVEL"`
	// RuleText     string      `json:"RULE_TEXT"`
	// TripType     string      `json:"TRIP_TYPE"`
	// AmountMin    json.Number `json:"AMOUNT_MIN"`
	// AmountMax    json.Number `json:"AMOUNT_MAX"`
	// Waers        string      `json:"WAERS"`
	// GradeLvlMin  string      `json:"GRADE_LVL_MIN"`
	// GradeLvlMax  string      `json:"GRADE_LVL_MAX"`
	// DeptID       string      `json:"DEPT_ID"`
	// WBSOnly      bool        `json:"WBS_ONLY"`
	// ApproverType string      `json:"APPROVER_TYPE"`
	// ApproverRole string      `json:"APPROVER_ROLE"`
	// ApproverEmp  string      `json:"APPROVER_EMP"`
	// SLAHours     int32       `json:"SLA_HOURS"`
	// ValidFrom    string      `json:"VALID_FROM"`
	// ValidTo      string      `json:"VALID_TO"`
	// IsActive     bool        `json:"IS_ACTIVE"`
}

func aprRulesMaxHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getAPRMaxRules(w, r)
	default:
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func validateAPRMaxQueryParams(client, ruleID string) error {
	if client != "" && utf8.RuneCountInString(client) != 3 {
		return fmt.Errorf("client must be exactly 3 characters")
	}
	if ruleID != "" && utf8.RuneCountInString(ruleID) != 4 {
		return fmt.Errorf("rule_id must be exactly 4 characters")
	}
	return nil
}

func getAPRMaxRules(w http.ResponseWriter, r *http.Request) {
	client := strings.TrimSpace(r.URL.Query().Get("client"))
	ruleID := strings.TrimSpace(r.URL.Query().Get("rule_id"))
	err := validateAPRMaxQueryParams(client, ruleID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if client != "" && utf8.RuneCountInString(client) != 3 {
		writeError(w, http.StatusBadRequest, "client must be exactly 3 characters")
		return
	}
	if ruleID != "" {
		var err error
		ruleID, err = normalizeNUMC("rule_id", ruleID, 4)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	rows, err := db.QueryContext(r.Context(), `
		SELECT
			COALESCE(rule_id, '')
		FROM travel.ztb_apr_rule
		ORDER BY client, rule_id DESC
		LIMIT 1

	`)
	if err != nil {
		log.Printf("query ZTB_APR_RULE: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not retrieve approval rules")
		return
	}
	defer rows.Close()

	rules := make([]APRRuleMax, 0)
	for rows.Next() {
		var rule APRRuleMax
		if err := rows.Scan(
			&rule.RuleID,
		); err != nil {
			log.Printf("scan ZTB_APR_RULE row: %v", err)
			writeError(w, http.StatusInternalServerError, "Could not retrieve approval rules")
			return
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate ZTB_APR_RULE rows: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not retrieve approval rules")
		return
	}

	writeJSON(w, http.StatusOK, rules)
}

func normalizeAPRMaxRule(rule *APRRule) error {
	rule.Client = strings.TrimSpace(rule.Client)
	if utf8.RuneCountInString(rule.Client) != 3 {
		return fmt.Errorf("CLIENT is required and must be exactly 3 characters")
	}

	var err error
	rule.RuleID, err = normalizeNUMC("RULE_ID", strings.TrimSpace(rule.RuleID), 4)
	if err != nil {
		return err
	}
	rule.ApprLevel, err = normalizeNUMCDefault("APPR_LEVEL", rule.ApprLevel, 2)
	if err != nil {
		return err
	}
	rule.GradeLvlMin, err = normalizeNUMCDefault("GRADE_LVL_MIN", rule.GradeLvlMin, 2)
	if err != nil {
		return err
	}
	rule.GradeLvlMax, err = normalizeNUMCDefault("GRADE_LVL_MAX", rule.GradeLvlMax, 2)
	if err != nil {
		return err
	}

	for _, field := range []struct {
		name  string
		value *string
		max   int
	}{
		{"BUKRS", &rule.Bukrs, 4},
		{"DOC_CAT", &rule.DocCat, 2},
		{"RULE_TEXT", &rule.RuleText, 60},
		{"TRIP_TYPE", &rule.TripType, 1},
		{"WAERS", &rule.Waers, 5},
		{"DEPT_ID", &rule.DeptID, 10},
		{"APPROVER_TYPE", &rule.ApproverType, 4},
		{"APPROVER_ROLE", &rule.ApproverRole, 10},
		{"APPROVER_EMP", &rule.ApproverEmp, 10},
	} {
		*field.value = strings.TrimSpace(*field.value)
		if utf8.RuneCountInString(*field.value) > field.max {
			return fmt.Errorf("%s exceeds %d characters", field.name, field.max)
		}
	}

	if rule.AmountMin == "" {
		rule.AmountMin = "0.00"
	}
	if rule.AmountMax == "" {
		rule.AmountMax = "0.00"
	}
	if !decimalPattern.MatchString(rule.AmountMin.String()) {
		return fmt.Errorf("AMOUNT_MIN must fit DEC(15,2)")
	}
	if !decimalPattern.MatchString(rule.AmountMax.String()) {
		return fmt.Errorf("AMOUNT_MAX must fit DEC(15,2)")
	}
	if rule.ValidFrom == "" {
		rule.ValidFrom = "00000000"
	}
	if rule.ValidTo == "" {
		rule.ValidTo = "00000000"
	}
	for _, field := range []struct {
		name  string
		value *string
	}{{"VALID_FROM", &rule.ValidFrom}, {"VALID_TO", &rule.ValidTo}} {
		*field.value, err = normalizeSAPDate(field.name, *field.value)
		if err != nil {
			return err
		}
	}
	return nil
}
