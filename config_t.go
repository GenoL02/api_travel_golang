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

type ConfigT struct {
	Client      string `json:"CLIENT"`
	Bukrs       string `json:"BUKRS"`
	Param_name  string `json:"PARAM_NAME"`
	Param_value string `json:"PARAM_VALUE"`
	Param_text  string `json:"PARAM_TEXT"`
}

const createConfigTTableSQL = `
CREATE TABLE IF NOT EXISTS travel.ztb_config (
	client      VARCHAR(3)  NOT NULL CHECK (char_length(client) = 3),
	bukrs       VARCHAR(4)  NOT NULL CHECK (bukrs <> ''),
	param_name  VARCHAR(30) NOT NULL CHECK (param_name <> ''),
	param_value VARCHAR(255) NOT NULL DEFAULT '',
	param_text  VARCHAR(100) NOT NULL DEFAULT '',
	PRIMARY KEY (client, bukrs, param_name)
)`

func createConfigTTable(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, createConfigTTableSQL)
	return err
}

func configTHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getConfigT(w, r)
	case http.MethodPost:
		createConfigT(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getConfigT(w http.ResponseWriter, r *http.Request) {
	client := strings.TrimSpace(r.URL.Query().Get("client"))
	bukrs := strings.TrimSpace(r.URL.Query().Get("bukrs"))
	paramName := strings.TrimSpace(r.URL.Query().Get("param_name"))
	if client != "" && utf8.RuneCountInString(client) != 3 {
		writeError(w, http.StatusBadRequest, "client must be exactly 3 characters")
		return
	}

	rows, err := db.QueryContext(r.Context(), `
		SELECT
			COALESCE(client, ''),
			COALESCE(bukrs, ''),
			COALESCE(param_name, ''),
			COALESCE(param_text, ''),
			COALESCE(param_value, '')
		FROM travel.ztb_config
		WHERE ($1 = '' OR client = $1)
		  AND ($2 = '' OR bukrs = $2)
		  AND ($3 = '' OR param_name = $3)
		ORDER BY client, bukrs, param_name
	`, client, bukrs, paramName)
	if err != nil {
		log.Printf("query ztb_config: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not retrieve codes")
		return
	}
	defer rows.Close()

	codes := make([]ConfigT, 0)
	for rows.Next() {
		var configT ConfigT
		if err := rows.Scan(
			&configT.Client, &configT.Bukrs, &configT.Param_name, &configT.Param_text,
			&configT.Param_value,
		); err != nil {
			log.Printf("scan ztb_config row: %v", err)
			writeError(w, http.StatusInternalServerError, "Could not retrieve codes")
			return
		}
		codes = append(codes, configT)
	}
	if err := rows.Err(); err != nil {
		log.Printf("iterate ztb_config rows: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not retrieve codes")
		return
	}

	writeJSON(w, http.StatusOK, codes)
}

func createConfigT(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var configT ConfigT
	if err := decoder.Decode(&configT); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Request body must contain a single JSON object")
		return
	}
	if err := normalizeConfigT(&configT); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	_, err := db.ExecContext(r.Context(), `
		INSERT INTO travel.ztb_config (
			bukrs, param_name, param_value, param_text, param_value
		) VALUES ($1, $2, $3, $4, $5)
	`, configT.Bukrs, configT.Param_name, configT.Param_value, configT.Param_text, configT.Param_value)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "A configuration with this BUKRS and PARAM_NAME already exists")
			return
		}
		log.Printf("insert ztb_config row: %v", err)
		writeError(w, http.StatusInternalServerError, "Could not create code")
		return
	}

	writeJSON(w, http.StatusCreated, configT)
}

func normalizeConfigT(configT *ConfigT) error {
	configT.Bukrs = strings.TrimSpace(configT.Bukrs)
	configT.Param_name = strings.TrimSpace(configT.Param_name)
	configT.Param_value = strings.TrimSpace(configT.Param_value)
	configT.Param_text = strings.TrimSpace(configT.Param_text)

	if utf8.RuneCountInString(configT.Bukrs) != 4 {
		return fmt.Errorf("BUKRS is required and must be exactly 4 characters")
	}
	if configT.Param_name == "" || utf8.RuneCountInString(configT.Param_name) > 30 {
		return fmt.Errorf("PARAM_NAME is required and must not exceed 30 characters")
	}
	if utf8.RuneCountInString(configT.Param_value) > 255 {
		return fmt.Errorf("PARAM_VALUE must not exceed 255 characters")
	}
	if utf8.RuneCountInString(configT.Param_text) > 100 {
		return fmt.Errorf("PARAM_TEXT must not exceed 100 characters")
	}

	return nil
}
