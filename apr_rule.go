package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

type APRRule struct {
	Client       string      `json:"CLIENT"`
	RuleID       string      `json:"RULE_ID"`
	Bukrs        string      `json:"BUKRS"`
	DocCat       string      `json:"DOC_CAT"`
	ApprLevel    string      `json:"APPR_LEVEL"`
	RuleText     string      `json:"RULE_TEXT"`
	TripType     string      `json:"TRIP_TYPE"`
	AmountMin    json.Number `json:"AMOUNT_MIN"`
	AmountMax    json.Number `json:"AMOUNT_MAX"`
	Waers        string      `json:"WAERS"`
	GradeLvlMin  string      `json:"GRADE_LVL_MIN"`
	GradeLvlMax  string      `json:"GRADE_LVL_MAX"`
	DeptID       string      `json:"DEPT_ID"`
	WBSOnly      bool        `json:"WBS_ONLY"`
	ApproverType string      `json:"APPROVER_TYPE"`
	ApproverRole string      `json:"APPROVER_ROLE"`
	ApproverEmp  string      `json:"APPROVER_EMP"`
	SLAHours     int32       `json:"SLA_HOURS"`
	ValidFrom    string      `json:"VALID_FROM"`
	ValidTo      string      `json:"VALID_TO"`
	IsActive     bool        `json:"IS_ACTIVE"`
}

const createAPRRuleTableSQL = `
CREATE TABLE IF NOT EXISTS travel.ztb_apr_rule (
	client         VARCHAR(3)  NOT NULL CHECK (char_length(client) = 3),
	rule_id        VARCHAR(4)  NOT NULL CHECK (rule_id ~ '^[0-9]{4}$'),
	bukrs          VARCHAR(4)  NOT NULL DEFAULT '',
	doc_cat        VARCHAR(2)  NOT NULL DEFAULT '',
	appr_level     VARCHAR(2)  NOT NULL DEFAULT '00' CHECK (appr_level ~ '^[0-9]{2}$'),
	rule_text      VARCHAR(60) NOT NULL DEFAULT '',
	trip_type      VARCHAR(1)  NOT NULL DEFAULT '',
	amount_min     NUMERIC(15,2) NOT NULL DEFAULT 0,
	amount_max     NUMERIC(15,2) NOT NULL DEFAULT 0,
	waers          VARCHAR(5)  NOT NULL DEFAULT '',
	grade_lvl_min  VARCHAR(2)  NOT NULL DEFAULT '00' CHECK (grade_lvl_min ~ '^[0-9]{2}$'),
	grade_lvl_max  VARCHAR(2)  NOT NULL DEFAULT '00' CHECK (grade_lvl_max ~ '^[0-9]{2}$'),
	dept_id        VARCHAR(10) NOT NULL DEFAULT '',
	wbs_only       CHAR(1)     NOT NULL DEFAULT ' ' CHECK (wbs_only IN ('X', ' ')),
	approver_type  VARCHAR(4)  NOT NULL DEFAULT '',
	approver_role  VARCHAR(10) NOT NULL DEFAULT '',
	approver_emp   VARCHAR(10) NOT NULL DEFAULT '',
	sla_hours      INTEGER     NOT NULL DEFAULT 0,
	valid_from     DATE,
	valid_to       DATE,
	is_active      CHAR(1)     NOT NULL DEFAULT ' ' CHECK (is_active IN ('X', ' ')),
	PRIMARY KEY (client, rule_id)
)`

var numcPattern = regexp.MustCompile(`^[0-9]+$`)
var decimalPattern = regexp.MustCompile(`^-?[0-9]{1,13}(\.[0-9]{1,2})?$`)

func createAPRRuleTable(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, createAPRRuleTableSQL)
	return err
}

func aprRulesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getAPRRules(w, r)
	case http.MethodPost:
		createAPRRule(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getAPRRules(w http.ResponseWriter, r *http.Request) {
	client := strings.TrimSpace(r.URL.Query().Get("client"))
	ruleID := strings.TrimSpace(r.URL.Query().Get("rule_id"))
	isActive := strings.TrimSpace(r.URL.Query().Get("is_active"))
	bukrs := strings.TrimSpace(r.URL.Query().Get("bukrs"))
	currentDate, err := resolveCurrentDate(r.URL.Query().Get("current_date"), time.Now())
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
			COALESCE(client, ''),
			COALESCE(rule_id, ''),
			COALESCE(bukrs, ''),
			COALESCE(doc_cat, ''),
			COALESCE(appr_level, '00'),
			COALESCE(rule_text, ''),
			COALESCE(trip_type, ''),
			COALESCE(amount_min, 0)::text,
			COALESCE(amount_max, 0)::text,
			COALESCE(waers, ''),
			COALESCE(grade_lvl_min, '00'),
			COALESCE(grade_lvl_max, '00'),
			COALESCE(dept_id, ''),
			COALESCE(wbs_only, ' '),
			COALESCE(approver_type, ''),
			COALESCE(approver_role, ''),
			COALESCE(approver_emp, ''),
			COALESCE(sla_hours, 0),
			COALESCE(to_char(valid_from, 'YYYY-MM-DD'), '00000000'),
			COALESCE(to_char(valid_to, 'YYYY-MM-DD'), '00000000'),
			COALESCE(is_active, ' ')
		FROM travel.ztb_apr_rule
		WHERE ($1 = '' OR client = $1)
		  AND ($2 = '' OR rule_id = $2)
		  AND ($3 = '' OR bukrs = $3)
		  AND ($4 = '' OR is_active = $4)
		  AND (valid_from IS NULL OR valid_from <= $5::date)
		  AND (valid_to IS NULL OR valid_to >= $5::date)
		ORDER BY client, rule_id

	`, client, ruleID, bukrs, isActive, currentDate)
	if err != nil {
		log.Printf("query ZTB_APR_RULE: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not retrieve approval rules")
		return
	}
	defer rows.Close()

	rules := make([]APRRule, 0)
	for rows.Next() {
		var rule APRRule
		var amountMin, amountMax, wbsOnly, isActive string
		if err := rows.Scan(
			&rule.Client, &rule.RuleID, &rule.Bukrs, &rule.DocCat,
			&rule.ApprLevel, &rule.RuleText, &rule.TripType,
			&amountMin, &amountMax, &rule.Waers, &rule.GradeLvlMin,
			&rule.GradeLvlMax, &rule.DeptID, &wbsOnly, &rule.ApproverType,
			&rule.ApproverRole, &rule.ApproverEmp, &rule.SLAHours,
			&rule.ValidFrom, &rule.ValidTo, &isActive,
		); err != nil {
			log.Printf("scan ZTB_APR_RULE row: %v", err)
			writeError(w, http.StatusInternalServerError, "Could not retrieve approval rules")
			return
		}
		rule.AmountMin = json.Number(amountMin)
		rule.AmountMax = json.Number(amountMax)
		rule.WBSOnly = wbsOnly == "X"
		rule.IsActive = isActive == "X"
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate ZTB_APR_RULE rows: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not retrieve approval rules")
		return
	}

	writeJSON(w, http.StatusOK, rules)
}

func createAPRRule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var rule APRRule
	if err := decoder.Decode(&rule); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Request body must contain a single JSON object")
		return
	}
	if err := normalizeAPRRule(&rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	_, err := db.ExecContext(r.Context(), `
		INSERT INTO travel.ztb_apr_rule (
			client, rule_id, bukrs, doc_cat, appr_level, rule_text,
			trip_type, amount_min, amount_max, waers, grade_lvl_min,
			grade_lvl_max, dept_id, wbs_only, approver_type, approver_role,
			approver_emp, sla_hours, valid_from, valid_to, is_active
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16, $17, $18,
			NULLIF($19, '00000000')::date,
			NULLIF($20, '00000000')::date,
			$21
		)
	`,
		rule.Client, rule.RuleID, rule.Bukrs, rule.DocCat, rule.ApprLevel,
		rule.RuleText, rule.TripType, rule.AmountMin.String(), rule.AmountMax.String(),
		rule.Waers, rule.GradeLvlMin, rule.GradeLvlMax, rule.DeptID,
		boolToSAP(rule.WBSOnly), rule.ApproverType, rule.ApproverRole,
		rule.ApproverEmp, rule.SLAHours, rule.ValidFrom, rule.ValidTo,
		boolToSAP(rule.IsActive),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "An approval rule with this CLIENT and RULE_ID already exists")
			return
		}
		log.Printf("insert ZTB_APR_RULE row: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not create approval rule")
		return
	}

	writeJSON(w, http.StatusCreated, rule)
}

func normalizeAPRRule(rule *APRRule) error {
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

func normalizeSAPDate(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "00000000" {
		return "00000000", nil
	}

	for _, layout := range []string{"2006-01-02", "20060102"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("%s must be a valid date in YYYY-MM-DD or YYYYMMDD format", name)
}

func resolveCurrentDate(value string, now time.Time) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return now.Format("2006-01-02"), nil
	}
	currentDate, err := normalizeSAPDate("current_date", value)
	if err != nil {
		return "", err
	}
	if currentDate == "00000000" {
		return "", fmt.Errorf("current_date must be a real date")
	}
	return currentDate, nil
}

func normalizeNUMCDefault(name, value string, width int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.Repeat("0", width)
	}
	return normalizeNUMC(name, value, width)
}

func normalizeNUMC(name, value string, width int) (string, error) {
	if value == "" || len(value) > width || !numcPattern.MatchString(value) {
		return "", fmt.Errorf("%s must contain 1 to %d digits", name, width)
	}
	return strings.Repeat("0", width-len(value)) + value, nil
}

func boolToSAP(value bool) string {
	if value {
		return "X"
	}
	return " "
}
