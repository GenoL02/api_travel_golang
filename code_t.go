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
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

type CodeT struct {
	Client      string `json:"CLIENT"`
	CodeType    string `json:"CODE_TYPE"`
	Code        string `json:"CODE"`
	Langu       string `json:"LANGU"`
	CodeText    string `json:"CODE_TEXT"`
	SortNo      string `json:"SORT_NO"`
	Criticality string `json:"CRITICALITY"`
	IsActive    bool   `json:"IS_ACTIVE"`
}

const createCodeTTableSQL = `
CREATE TABLE IF NOT EXISTS travel.ztb_code_t (
	client      VARCHAR(3)  NOT NULL CHECK (char_length(client) = 3),
	code_type   VARCHAR(20) NOT NULL CHECK (code_type <> ''),
	code        VARCHAR(10) NOT NULL CHECK (code <> ''),
	langu       VARCHAR(1)  NOT NULL CHECK (char_length(langu) = 1),
	code_text   VARCHAR(60) NOT NULL DEFAULT '',
	sort_no     VARCHAR(3)  NOT NULL DEFAULT '000' CHECK (sort_no ~ '^[0-9]{3}$'),
	criticality VARCHAR(1)  NOT NULL DEFAULT '0' CHECK (criticality ~ '^[0-9]$'),
	is_active   CHAR(1)     NOT NULL DEFAULT ' ' CHECK (is_active IN ('X', ' ')),
	PRIMARY KEY (client, code_type, code, langu)
)`

func createCodeTTable(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, createCodeTTableSQL)
	return err
}

func codeTHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getCodeT(w, r)
	case http.MethodPost:
		createCodeT(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getCodeT(w http.ResponseWriter, r *http.Request) {
	client := strings.TrimSpace(r.URL.Query().Get("client"))
	codeType := strings.TrimSpace(r.URL.Query().Get("code_type"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	langu := strings.TrimSpace(r.URL.Query().Get("langu"))
	if client != "" && utf8.RuneCountInString(client) != 3 {
		writeError(w, http.StatusBadRequest, "client must be exactly 3 characters")
		return
	}
	if codeType != "" && utf8.RuneCountInString(codeType) > 20 {
		writeError(w, http.StatusBadRequest, "code_type must not exceed 20 characters")
		return
	}
	if code != "" && utf8.RuneCountInString(code) > 10 {
		writeError(w, http.StatusBadRequest, "code must not exceed 10 characters")
		return
	}
	if langu != "" && utf8.RuneCountInString(langu) != 1 {
		writeError(w, http.StatusBadRequest, "langu must be exactly 1 character")
		return
	}

	rows, err := db.QueryContext(r.Context(), `
		SELECT
			COALESCE(client, ''),
			COALESCE(code_type, ''),
			COALESCE(code, ''),
			COALESCE(langu, ''),
			COALESCE(code_text, ''),
			COALESCE(sort_no, '000'),
			COALESCE(criticality, '0'),
			COALESCE(is_active, ' ')
		FROM travel.ztb_code_t
		WHERE ($1 = '' OR client = $1)
		  AND ($2 = '' OR code_type = $2)
		  AND ($3 = '' OR code = $3)
		  AND ($4 = '' OR langu = $4)
		ORDER BY client, code_type, code, langu
	`, client, codeType, code, langu)
	if err != nil {
		log.Printf("query ZTB_CODE_T: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not retrieve codes")
		return
	}
	defer rows.Close()

	codes := make([]CodeT, 0)
	for rows.Next() {
		var codeT CodeT
		var isActive string
		if err := rows.Scan(
			&codeT.Client, &codeT.CodeType, &codeT.Code, &codeT.Langu,
			&codeT.CodeText, &codeT.SortNo, &codeT.Criticality, &isActive,
		); err != nil {
			log.Printf("scan ZTB_CODE_T row: %v", err)
			writeError(w, http.StatusInternalServerError, "Could not retrieve codes")
			return
		}
		codeT.IsActive = isActive == "X"
		codes = append(codes, codeT)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate ZTB_CODE_T rows: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not retrieve codes")
		return
	}

	writeJSON(w, http.StatusOK, codes)
}

func createCodeT(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var codeT CodeT
	if err := decoder.Decode(&codeT); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Request body must contain a single JSON object")
		return
	}
	if err := normalizeCodeT(&codeT); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	_, err := db.ExecContext(r.Context(), `
		INSERT INTO travel.ztb_code_t (
			client, code_type, code, langu, code_text, sort_no, criticality, is_active
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, codeT.Client, codeT.CodeType, codeT.Code, codeT.Langu,
		codeT.CodeText, codeT.SortNo, codeT.Criticality, boolToSAP(codeT.IsActive))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "A code with this CLIENT, CODE_TYPE, CODE, and LANGU already exists")
			return
		}
		log.Printf("insert ZTB_CODE_T row: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not create code")
		return
	}

	writeJSON(w, http.StatusCreated, codeT)
}

func normalizeCodeT(codeT *CodeT) error {
	codeT.Client = strings.TrimSpace(codeT.Client)
	codeT.CodeType = strings.TrimSpace(codeT.CodeType)
	codeT.Code = strings.TrimSpace(codeT.Code)
	codeT.Langu = strings.TrimSpace(codeT.Langu)
	codeT.CodeText = strings.TrimSpace(codeT.CodeText)

	if utf8.RuneCountInString(codeT.Client) != 3 {
		return fmt.Errorf("CLIENT is required and must be exactly 3 characters")
	}
	if codeT.CodeType == "" || utf8.RuneCountInString(codeT.CodeType) > 20 {
		return fmt.Errorf("CODE_TYPE is required and must not exceed 20 characters")
	}
	if codeT.Code == "" || utf8.RuneCountInString(codeT.Code) > 10 {
		return fmt.Errorf("CODE is required and must not exceed 10 characters")
	}
	if utf8.RuneCountInString(codeT.Langu) != 1 {
		return fmt.Errorf("LANGU is required and must be exactly 1 character")
	}
	if utf8.RuneCountInString(codeT.CodeText) > 60 {
		return fmt.Errorf("CODE_TEXT must not exceed 60 characters")
	}

	var err error
	codeT.SortNo, err = normalizeNUMCDefault("SORT_NO", codeT.SortNo, 3)
	if err != nil {
		return err
	}
	codeT.Criticality, err = normalizeNUMCDefault("CRITICALITY", codeT.Criticality, 1)
	if err != nil {
		return err
	}
	return nil
}
