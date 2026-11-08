package main

import (
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type Group struct {
	ID            int64
	Name          string
	EmployeeCount int64
}

type Project struct {
	ID   int64
	Name string
	Code string
}

type Employee struct {
	ID               int64
	Login            string
	PasswordHash     string
	LastName         string
	FirstName        string
	MiddleName       string
	GroupID          int64
	GroupName        string
	WorkEntriesCount int64
}

type WorkEntry struct {
	ID                 int64
	EmployeeID         int64
	EmployeeLogin      string
	EmployeeLastName   string
	EmployeeFirstName  string
	EmployeeMiddleName string
	GroupName          string
	ProjectID          int64
	ProjectName        string
	ProjectCode        string
	WorkDate           string
	StartTime          string
	DurationMinutes    int64
	Comment            string
}

var (
	errDuplicateGroup        = errors.New("группа с таким наименованием уже существует")
	errDuplicateLogin        = errors.New("сотрудник с таким логином уже существует")
	errGroupNotFound         = errors.New("выбранный отдел не найден")
	errEmployeeNotFound      = errors.New("сотрудник не найден")
	errGroupHasEmployees     = errors.New("к отделу прикреплены сотрудники")
	errDuplicateProjectCode  = errors.New("проект с таким кодовым обозначением уже существует")
	errProjectNotFound       = errors.New("проект не найден")
	errProjectHasWorkEntries = errors.New("на проект ссылаются записи табеля")
	errWorkEntryInvalidRef   = errors.New("некорректная ссылка на сотрудника или проект")
	errWorkEntryNotFound     = errors.New("запись табеля не найдена")
)

func initDB(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	_, statErr := os.Stat(path)
	created := os.IsNotExist(statErr)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if created {
		log.Printf("файл БД не найден, создан новый: %s", path)
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS groups (
			id   INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL COLLATE NOCASE UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id   INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			code TEXT NOT NULL COLLATE NOCASE UNIQUE
		)`,
		`CREATE TABLE IF NOT EXISTS employees (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			login         TEXT NOT NULL COLLATE NOCASE UNIQUE,
			password_hash TEXT NOT NULL,
			last_name     TEXT NOT NULL,
			first_name    TEXT NOT NULL,
			middle_name   TEXT NOT NULL DEFAULT '',
			group_id      INTEGER NOT NULL REFERENCES groups(id)
		)`,
		`CREATE TABLE IF NOT EXISTS work_entries (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			employee_id      INTEGER NOT NULL REFERENCES employees(id),
			project_id       INTEGER NOT NULL REFERENCES projects(id),
			work_date        TEXT    NOT NULL,
			start_time       TEXT    NOT NULL,
			duration_minutes INTEGER NOT NULL CHECK (duration_minutes > 0),
			comment          TEXT    NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_entries_employee_date
			ON work_entries(employee_id, work_date)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func listGroups(db *sql.DB, dir string) ([]Group, error) {
	order := "ASC"
	if dir == "desc" {
		order = "DESC"
	}
	query := `SELECT g.id, g.name, COALESCE(cnt.c, 0) AS employee_count
		FROM groups g
		LEFT JOIN (SELECT group_id, COUNT(*) AS c FROM employees GROUP BY group_id) cnt
			ON cnt.group_id = g.id
		ORDER BY g.name ` + order
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.EmployeeCount); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func getGroup(db *sql.DB, id int64) (*Group, error) {
	var g Group
	err := db.QueryRow(`SELECT id, name FROM groups WHERE id = ?`, id).Scan(&g.ID, &g.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func addGroup(db *sql.DB, name string) (int64, error) {
	res, err := db.Exec(`INSERT INTO groups (name) VALUES (?)`, name)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, errDuplicateGroup
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func updateGroup(db *sql.DB, g Group) error {
	res, err := db.Exec(`UPDATE groups SET name = ? WHERE id = ?`, g.Name, g.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return errDuplicateGroup
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errGroupNotFound
	}
	return nil
}

func deleteGroup(db *sql.DB, id int64) error {
	var cnt int
	err := db.QueryRow(`SELECT COUNT(*) FROM employees WHERE group_id = ?`, id).Scan(&cnt)
	if err != nil {
		return err
	}
	if cnt > 0 {
		return errGroupHasEmployees
	}
	res, err := db.Exec(`DELETE FROM groups WHERE id = ?`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return errGroupHasEmployees
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errGroupNotFound
	}
	return nil
}

func groupExists(db *sql.DB, id int64) (bool, error) {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM groups WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func listProjects(db *sql.DB, sortField, dir string) ([]Project, error) {
	col := "name"
	if sortField == "code" {
		col = "code"
	}
	order := "ASC"
	if dir == "desc" {
		order = "DESC"
	}
	query := `SELECT id, name, code FROM projects ORDER BY ` + col + ` ` + order + `, id ASC`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Code); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func getProject(db *sql.DB, id int64) (*Project, error) {
	var p Project
	err := db.QueryRow(`SELECT id, name, code FROM projects WHERE id = ?`, id).Scan(&p.ID, &p.Name, &p.Code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func addProject(db *sql.DB, p Project) (int64, error) {
	res, err := db.Exec(`INSERT INTO projects (name, code) VALUES (?, ?)`, p.Name, p.Code)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, errDuplicateProjectCode
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func updateProject(db *sql.DB, p Project) error {
	res, err := db.Exec(`UPDATE projects SET name = ?, code = ? WHERE id = ?`, p.Name, p.Code, p.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return errDuplicateProjectCode
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errProjectNotFound
	}
	return nil
}

func deleteProject(db *sql.DB, id int64) error {
	var cnt int
	err := db.QueryRow(`SELECT COUNT(*) FROM work_entries WHERE project_id = ?`, id).Scan(&cnt)
	if err != nil {
		return err
	}
	if cnt > 0 {
		return errProjectHasWorkEntries
	}
	res, err := db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return errProjectHasWorkEntries
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errProjectNotFound
	}
	return nil
}

func listEmployees(db *sql.DB, sortField, dir string, groupID int64) ([]Employee, error) {
	col := "e.login"
	switch sortField {
	case "last_name":
		col = "e.last_name"
	case "group_name":
		col = "g.name"
	}
	order := "ASC"
	if dir == "desc" {
		order = "DESC"
	}
	query := `
		SELECT e.id, e.login, e.last_name, e.first_name, e.middle_name, e.group_id, g.name,
		       COALESCE(cnt.c, 0) AS work_entries_count
		FROM employees e
		JOIN groups g ON g.id = e.group_id
		LEFT JOIN (SELECT employee_id, COUNT(*) AS c FROM work_entries GROUP BY employee_id) cnt
			ON cnt.employee_id = e.id
		WHERE (? = 0 OR e.group_id = ?)
		ORDER BY ` + col + ` ` + order + `, e.id ASC`
	rows, err := db.Query(query, groupID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var employees []Employee
	for rows.Next() {
		var e Employee
		if err := rows.Scan(&e.ID, &e.Login, &e.LastName, &e.FirstName, &e.MiddleName, &e.GroupID, &e.GroupName, &e.WorkEntriesCount); err != nil {
			return nil, err
		}
		employees = append(employees, e)
	}
	return employees, rows.Err()
}

func getEmployee(db *sql.DB, id int64) (*Employee, error) {
	var e Employee
	err := db.QueryRow(`
		SELECT e.id, e.login, e.last_name, e.first_name, e.middle_name, e.group_id, g.name
		FROM employees e
		JOIN groups g ON g.id = e.group_id
		WHERE e.id = ?
	`, id).Scan(&e.ID, &e.Login, &e.LastName, &e.FirstName, &e.MiddleName, &e.GroupID, &e.GroupName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errEmployeeNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func getEmployeeByLogin(db *sql.DB, login string) (*Employee, error) {
	var e Employee
	err := db.QueryRow(`
		SELECT e.id, e.login, e.password_hash, e.last_name, e.first_name, e.middle_name, e.group_id, g.name
		FROM employees e
		JOIN groups g ON g.id = e.group_id
		WHERE e.login = ?
	`, login).Scan(&e.ID, &e.Login, &e.PasswordHash, &e.LastName, &e.FirstName, &e.MiddleName, &e.GroupID, &e.GroupName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errEmployeeNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func updateEmployee(db *sql.DB, e Employee, password string) error {
	ok, err := groupExists(db, e.GroupID)
	if err != nil {
		return err
	}
	if !ok {
		return errGroupNotFound
	}
	var res sql.Result
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		res, err = db.Exec(`
			UPDATE employees
			SET last_name = ?, first_name = ?, middle_name = ?, group_id = ?, password_hash = ?
			WHERE id = ?
		`, e.LastName, e.FirstName, e.MiddleName, e.GroupID, string(hash), e.ID)
		if err != nil {
			return err
		}
	} else {
		res, err = db.Exec(`
			UPDATE employees
			SET last_name = ?, first_name = ?, middle_name = ?, group_id = ?
			WHERE id = ?
		`, e.LastName, e.FirstName, e.MiddleName, e.GroupID, e.ID)
		if err != nil {
			return err
		}
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errEmployeeNotFound
	}
	return nil
}

func deleteEmployee(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM employees WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errEmployeeNotFound
	}
	return nil
}

func addWorkEntry(db *sql.DB, e WorkEntry) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO work_entries (employee_id, project_id, work_date, start_time, duration_minutes, comment)
		VALUES (?, ?, ?, ?, ?, ?)
	`, e.EmployeeID, e.ProjectID, e.WorkDate, e.StartTime, e.DurationMinutes, e.Comment)
	if err != nil {
		if isForeignKeyViolation(err) {
			return 0, errWorkEntryInvalidRef
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func listWorkEntriesByEmployee(db *sql.DB, employeeID int64) ([]WorkEntry, error) {
	rows, err := db.Query(`
		SELECT w.id, w.employee_id, e.login, e.last_name, e.first_name, e.middle_name, g.name,
		       w.project_id, p.name, p.code,
		       w.work_date, w.start_time, w.duration_minutes, w.comment
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		JOIN groups g ON g.id = e.group_id
		JOIN projects  p ON p.id = w.project_id
		WHERE w.employee_id = ?
		ORDER BY w.work_date DESC, w.start_time DESC, w.id DESC
	`, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []WorkEntry
	for rows.Next() {
		var w WorkEntry
		if err := rows.Scan(&w.ID, &w.EmployeeID, &w.EmployeeLogin,
			&w.EmployeeLastName, &w.EmployeeFirstName, &w.EmployeeMiddleName, &w.GroupName,
			&w.ProjectID, &w.ProjectName, &w.ProjectCode,
			&w.WorkDate, &w.StartTime, &w.DurationMinutes, &w.Comment); err != nil {
			return nil, err
		}
		entries = append(entries, w)
	}
	return entries, rows.Err()
}

func listWorkEntriesByMonth(db *sql.DB, employeeID int64, month string) ([]WorkEntry, error) {
	rows, err := db.Query(`
		SELECT w.id, w.employee_id, e.login, e.last_name, e.first_name, e.middle_name, g.name,
		       w.project_id, p.name, p.code,
		       w.work_date, w.start_time, w.duration_minutes, w.comment
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		JOIN groups g ON g.id = e.group_id
		JOIN projects  p ON p.id = w.project_id
		WHERE w.employee_id = ? AND substr(w.work_date, 1, 7) = ?
		ORDER BY w.work_date DESC, w.start_time DESC, w.id DESC
	`, employeeID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []WorkEntry
	for rows.Next() {
		var w WorkEntry
		if err := rows.Scan(&w.ID, &w.EmployeeID, &w.EmployeeLogin,
			&w.EmployeeLastName, &w.EmployeeFirstName, &w.EmployeeMiddleName, &w.GroupName,
			&w.ProjectID, &w.ProjectName, &w.ProjectCode,
			&w.WorkDate, &w.StartTime, &w.DurationMinutes, &w.Comment); err != nil {
			return nil, err
		}
		entries = append(entries, w)
	}
	return entries, rows.Err()
}

func listWorkEntryMonths(db *sql.DB, employeeID int64) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT substr(work_date, 1, 7) AS m
		FROM work_entries
		WHERE employee_id = ?
		ORDER BY m
	`, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var months []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		months = append(months, m)
	}
	return months, rows.Err()
}

func getLastWorkEntry(db *sql.DB, employeeID int64) (*WorkEntry, error) {
	var w WorkEntry
	err := db.QueryRow(`
		SELECT w.id, w.employee_id, e.login, e.last_name, e.first_name, e.middle_name, g.name,
		       w.project_id, p.name, p.code,
		       w.work_date, w.start_time, w.duration_minutes, w.comment
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		JOIN groups g ON g.id = e.group_id
		JOIN projects  p ON p.id = w.project_id
		WHERE w.employee_id = ?
		ORDER BY w.work_date DESC, w.start_time DESC, w.id DESC
		LIMIT 1
	`, employeeID).Scan(&w.ID, &w.EmployeeID, &w.EmployeeLogin,
		&w.EmployeeLastName, &w.EmployeeFirstName, &w.EmployeeMiddleName, &w.GroupName,
		&w.ProjectID, &w.ProjectName, &w.ProjectCode,
		&w.WorkDate, &w.StartTime, &w.DurationMinutes, &w.Comment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func getLastOtherWorkEntry(db *sql.DB, employeeID, excludeID int64) (*WorkEntry, error) {
	var w WorkEntry
	err := db.QueryRow(`
		SELECT w.id, w.employee_id, e.login, e.last_name, e.first_name, e.middle_name, g.name,
		       w.project_id, p.name, p.code,
		       w.work_date, w.start_time, w.duration_minutes, w.comment
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		JOIN groups g ON g.id = e.group_id
		JOIN projects  p ON p.id = w.project_id
		WHERE w.employee_id = ? AND w.id <> ?
		ORDER BY w.work_date DESC, w.start_time DESC, w.id DESC
		LIMIT 1
	`, employeeID, excludeID).Scan(&w.ID, &w.EmployeeID, &w.EmployeeLogin,
		&w.EmployeeLastName, &w.EmployeeFirstName, &w.EmployeeMiddleName, &w.GroupName,
		&w.ProjectID, &w.ProjectName, &w.ProjectCode,
		&w.WorkDate, &w.StartTime, &w.DurationMinutes, &w.Comment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func getWorkEntry(db *sql.DB, id int64) (*WorkEntry, error) {
	var w WorkEntry
	err := db.QueryRow(`
		SELECT w.id, w.employee_id, e.login, e.last_name, e.first_name, e.middle_name, g.name,
		       w.project_id, p.name, p.code,
		       w.work_date, w.start_time, w.duration_minutes, w.comment
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		JOIN groups g ON g.id = e.group_id
		JOIN projects  p ON p.id = w.project_id
		WHERE w.id = ?
	`, id).Scan(&w.ID, &w.EmployeeID, &w.EmployeeLogin,
		&w.EmployeeLastName, &w.EmployeeFirstName, &w.EmployeeMiddleName, &w.GroupName,
		&w.ProjectID, &w.ProjectName, &w.ProjectCode,
		&w.WorkDate, &w.StartTime, &w.DurationMinutes, &w.Comment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errWorkEntryNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func updateWorkEntry(db *sql.DB, w WorkEntry) error {
	res, err := db.Exec(`
		UPDATE work_entries
		SET project_id = ?, work_date = ?, start_time = ?, duration_minutes = ?, comment = ?
		WHERE id = ?
	`, w.ProjectID, w.WorkDate, w.StartTime, w.DurationMinutes, w.Comment, w.ID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return errWorkEntryInvalidRef
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errWorkEntryNotFound
	}
	return nil
}

func deleteWorkEntry(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM work_entries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errWorkEntryNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
			se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}

func isForeignKeyViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
	}
	return false
}

func listWorkEntriesByGroup(db *sql.DB, groupID int64) ([]WorkEntry, error) {
	rows, err := db.Query(`
		SELECT w.id, w.employee_id, e.login, e.last_name, e.first_name, e.middle_name, g.name,
		       w.project_id, p.name, p.code,
		       w.work_date, w.start_time, w.duration_minutes, w.comment
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		JOIN groups g ON g.id = e.group_id
		JOIN projects p ON p.id = w.project_id
		WHERE e.group_id = ?
		ORDER BY w.work_date ASC, w.start_time ASC, w.id ASC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []WorkEntry
	for rows.Next() {
		var w WorkEntry
		if err := rows.Scan(&w.ID, &w.EmployeeID, &w.EmployeeLogin,
			&w.EmployeeLastName, &w.EmployeeFirstName, &w.EmployeeMiddleName, &w.GroupName,
			&w.ProjectID, &w.ProjectName, &w.ProjectCode,
			&w.WorkDate, &w.StartTime, &w.DurationMinutes, &w.Comment); err != nil {
			return nil, err
		}
		entries = append(entries, w)
	}
	return entries, rows.Err()
}

func listGroupWorkMonths(db *sql.DB, groupID int64) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT substr(w.work_date, 1, 7) AS m
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		WHERE e.group_id = ?
		ORDER BY m
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var months []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		months = append(months, m)
	}
	return months, rows.Err()
}

func listWorkEntriesByGroupAndMonth(db *sql.DB, groupID int64, month string) ([]WorkEntry, error) {
	rows, err := db.Query(`
		SELECT w.id, w.employee_id, e.login, e.last_name, e.first_name, e.middle_name, g.name,
		       w.project_id, p.name, p.code,
		       w.work_date, w.start_time, w.duration_minutes, w.comment
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		JOIN groups g ON g.id = e.group_id
		JOIN projects p ON p.id = w.project_id
		WHERE e.group_id = ? AND substr(w.work_date, 1, 7) = ?
		ORDER BY e.last_name ASC, e.first_name ASC, w.work_date ASC
	`, groupID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []WorkEntry
	for rows.Next() {
		var w WorkEntry
		if err := w.Scan(rows); err != nil {
			return nil, err
		}
		entries = append(entries, w)
	}
	return entries, rows.Err()
}

func (w *WorkEntry) Scan(rows *sql.Rows) error {
	return rows.Scan(&w.ID, &w.EmployeeID, &w.EmployeeLogin,
		&w.EmployeeLastName, &w.EmployeeFirstName, &w.EmployeeMiddleName, &w.GroupName,
		&w.ProjectID, &w.ProjectName, &w.ProjectCode,
		&w.WorkDate, &w.StartTime, &w.DurationMinutes, &w.Comment)
}

func listWorkEntriesByEmployeeAndMonth(db *sql.DB, employeeID int64, month string) ([]WorkEntry, error) {
	rows, err := db.Query(`
		SELECT w.id, w.employee_id, e.login, e.last_name, e.first_name, e.middle_name, g.name,
		       w.project_id, p.name, p.code,
		       w.work_date, w.start_time, w.duration_minutes, w.comment
		FROM work_entries w
		JOIN employees e ON e.id = w.employee_id
		JOIN groups g ON g.id = e.group_id
		JOIN projects p ON p.id = w.project_id
		WHERE w.employee_id = ? AND substr(w.work_date, 1, 7) = ?
		ORDER BY w.work_date ASC, w.start_time ASC, w.id ASC
	`, employeeID, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []WorkEntry
	for rows.Next() {
		var w WorkEntry
		if err := rows.Scan(&w.ID, &w.EmployeeID, &w.EmployeeLogin,
			&w.EmployeeLastName, &w.EmployeeFirstName, &w.EmployeeMiddleName, &w.GroupName,
			&w.ProjectID, &w.ProjectName, &w.ProjectCode,
			&w.WorkDate, &w.StartTime, &w.DurationMinutes, &w.Comment); err != nil {
			return nil, err
		}
		entries = append(entries, w)
	}
	return entries, rows.Err()
}

func listEmployeeWorkMonths(db *sql.DB, employeeID int64) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT substr(work_date, 1, 7) AS m
		FROM work_entries
		WHERE employee_id = ?
		ORDER BY m
	`, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var months []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		months = append(months, m)
	}
	return months, rows.Err()
}