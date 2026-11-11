package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func (a *App) addEmployeeHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	groupsDir, groupsPage, projectsSort, projectsDir, projectsPage,
		employeesSort, employeesDir, employeesPage, employeesGroupID := configParamsFromQuery(r)
	employeesSearch := normalizeEmployeesSearch(r.URL.Query().Get("employees_search"))
	login := strings.TrimSpace(r.FormValue("login"))
	password := r.FormValue("password")
	lastName := strings.TrimSpace(r.FormValue("last_name"))
	firstName := strings.TrimSpace(r.FormValue("first_name"))
	middleName := strings.TrimSpace(r.FormValue("middle_name"))
	groupIDStr := strings.TrimSpace(r.FormValue("group_id"))
	target := targetEmployee(0, login)
	reRenderWithAdd := func(errMsg string) {
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.EmployeeError = errMsg
				d.AddLogin = login
				d.AddLastName = lastName
				d.AddFirstName = firstName
				d.AddMiddleName = middleName
				d.AddGroupID = parseOrZero(groupIDStr)
			})
	}
	if login == "" {
		auditFail(r, user, "create", target, "empty_login")
		reRenderWithAdd("Логин обязателен")
		return
	}
	if password == "" {
		auditFail(r, user, "create", target, "empty_password")
		reRenderWithAdd("Пароль обязателен")
		return
	}
	if lastName == "" {
		auditFail(r, user, "create", target, "empty_last_name")
		reRenderWithAdd("Фамилия обязательна")
		return
	}
	if firstName == "" {
		auditFail(r, user, "create", target, "empty_first_name")
		reRenderWithAdd("Имя обязательно")
		return
	}
	if groupIDStr == "" {
		auditFail(r, user, "create", target, "empty_group")
		reRenderWithAdd("Выберите отдел")
		return
	}
	if !isValidName(lastName) {
		auditFail(r, user, "create", target, "invalid_last_name")
		reRenderWithAdd("Фамилия может содержать только буквы, пробел и дефис")
		return
	}
	if !isValidName(firstName) {
		auditFail(r, user, "create", target, "invalid_first_name")
		reRenderWithAdd("Имя может содержать только буквы, пробел и дефис")
		return
	}
	if middleName != "" && !isValidName(middleName) {
		auditFail(r, user, "create", target, "invalid_middle_name")
		reRenderWithAdd("Отчество может содержать только буквы, пробел и дефис")
		return
	}
	if len([]rune(login)) > 100 || len([]rune(lastName)) > 100 ||
		len([]rune(firstName)) > 100 || len([]rune(middleName)) > 100 {
		auditFail(r, user, "create", target, "field_too_long")
		reRenderWithAdd("Превышена максимальная длина поля (100 символов)")
		return
	}
	if len([]rune(password)) < 4 || len([]rune(password)) > 100 {
		auditFail(r, user, "create", target, "invalid_password_length")
		reRenderWithAdd("Пароль должен содержать от 4 до 100 символов")
		return
	}
	groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
	if err != nil {
		auditFail(r, user, "create", target, "invalid_group_id")
		reRenderWithAdd("Некорректный отдел")
		return
	}
	emp := Employee{
		Login:      login,
		LastName:   lastName,
		FirstName:  firstName,
		MiddleName: middleName,
		GroupID:    groupID,
	}
	err = a.addEmployee(emp, password)
	switch {
	case err == nil:
		auditOK(r, user, "create", target)
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID, employeesSearch,
		), http.StatusSeeOther)
	case errors.Is(err, errDuplicateLogin):
		auditFail(r, user, "create", target, "duplicate_login")
		reRenderWithAdd("Сотрудник с таким логином уже существует")
	case errors.Is(err, errGroupNotFound):
		auditFail(r, user, "create", target, "group_not_found")
		reRenderWithAdd("Выбранный отдел не найден")
	default:
		log.Printf("ошибка вставки сотрудника: %v", err)
		auditFailF(r, user, "create", target, "db_error: %v", err)
		reRenderWithAdd("Не удалось сохранить сотрудника")
	}
}

func (a *App) addEmployee(e Employee, password string) error {
	ok, err := groupExists(a.db, e.GroupID)
	if err != nil {
		return err
	}
	if !ok {
		return errGroupNotFound
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`
		INSERT INTO employees (login, password_hash, last_name, first_name, middle_name, group_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, e.Login, string(hash), e.LastName, e.FirstName, e.MiddleName, e.GroupID)
	if err != nil {
		if isUniqueViolation(err) {
			return errDuplicateLogin
		}
		if isForeignKeyViolation(err) {
			return errGroupNotFound
		}
		return err
	}
	return nil
}

func (a *App) editEmployeeHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	groupsDir, groupsPage, projectsSort, projectsDir, projectsPage,
		employeesSort, employeesDir, employeesPage, employeesGroupID := configParamsFromQuery(r)
	employeesSearch := normalizeEmployeesSearch(r.URL.Query().Get("employees_search"))
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		auditFail(r, user, "update", "employee(id="+idStr+")", "invalid_id")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Некорректный идентификатор сотрудника"
			})
		return
	}
	lastName := strings.TrimSpace(r.FormValue("last_name"))
	firstName := strings.TrimSpace(r.FormValue("first_name"))
	middleName := strings.TrimSpace(r.FormValue("middle_name"))
	groupIDStr := strings.TrimSpace(r.FormValue("group_id"))
	password := r.FormValue("password")
	existing, gerr := getEmployee(a.db, id)
	if gerr != nil {
		if errors.Is(gerr, errEmployeeNotFound) {
			auditFail(r, user, "update", targetEmployee(id, ""), "not_found")
			a.renderConfigPageWith(w, user,
				groupsDir, groupsPage,
				projectsSort, projectsDir, projectsPage,
				employeesSort, employeesDir, employeesPage,
				employeesGroupID,
				employeesSearch,
				func(d *ConfigPageData) {
					d.PageError = "Сотрудник не найден"
				})
			return
		}
		log.Printf("ошибка чтения сотрудника: %v", gerr)
		auditFailF(r, user, "update", targetEmployee(id, ""), "db_error: %v", gerr)
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Не удалось сохранить изменения"
			})
		return
	}
	target := targetEmployee(id, existing.Login)
	reRenderWithEdit := func(errMsg string) {
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.EditEmployee = &Employee{
					ID:         id,
					LastName:   lastName,
					FirstName:  firstName,
					MiddleName: middleName,
					GroupID:    parseOrZero(groupIDStr),
				}
				d.EditError = errMsg
			})
	}
	if lastName == "" {
		auditFail(r, user, "update", target, "empty_last_name")
		reRenderWithEdit("Фамилия обязательна")
		return
	}
	if firstName == "" {
		auditFail(r, user, "update", target, "empty_first_name")
		reRenderWithEdit("Имя обязательно")
		return
	}
	if groupIDStr == "" {
		auditFail(r, user, "update", target, "empty_group")
		reRenderWithEdit("Выберите отдел")
		return
	}
	if !isValidName(lastName) {
		auditFail(r, user, "update", target, "invalid_last_name")
		reRenderWithEdit("Фамилия может содержать только буквы, пробел и дефис")
		return
	}
	if !isValidName(firstName) {
		auditFail(r, user, "update", target, "invalid_first_name")
		reRenderWithEdit("Имя может содержать только буквы, пробел и дефис")
		return
	}
	if middleName != "" && !isValidName(middleName) {
		auditFail(r, user, "update", target, "invalid_middle_name")
		reRenderWithEdit("Отчество может содержать только буквы, пробел и дефис")
		return
	}
	if len([]rune(lastName)) > 100 || len([]rune(firstName)) > 100 || len([]rune(middleName)) > 100 {
		auditFail(r, user, "update", target, "field_too_long")
		reRenderWithEdit("Превышена максимальная длина поля (100 символов)")
		return
	}
	if password != "" && (len([]rune(password)) < 4 || len([]rune(password)) > 100) {
		auditFail(r, user, "update", target, "invalid_password_length")
		reRenderWithEdit("Пароль должен содержать от 4 до 100 символов")
		return
	}
	groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
	if err != nil {
		auditFail(r, user, "update", target, "invalid_group_id")
		reRenderWithEdit("Некорректный отдел")
		return
	}
	emp := Employee{
		ID:         id,
		LastName:   lastName,
		FirstName:  firstName,
		MiddleName: middleName,
		GroupID:    groupID,
	}
	err = updateEmployee(a.db, emp, password)
	switch {
	case err == nil:
		auditOK(r, user, "update", target)
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID, employeesSearch,
		), http.StatusSeeOther)
	case errors.Is(err, errGroupNotFound):
		auditFail(r, user, "update", target, "group_not_found")
		reRenderWithEdit("Выбранный отдел не найден")
	case errors.Is(err, errEmployeeNotFound):
		auditFail(r, user, "update", target, "not_found")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Сотрудник не найден"
			})
	default:
		log.Printf("ошибка обновления сотрудника: %v", err)
		auditFailF(r, user, "update", target, "db_error: %v", err)
		reRenderWithEdit("Не удалось сохранить изменения")
	}
}

func (a *App) deleteEmployeeHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	groupsDir, groupsPage, projectsSort, projectsDir, projectsPage,
		employeesSort, employeesDir, employeesPage, employeesGroupID := configParamsFromQuery(r)
	employeesSearch := normalizeEmployeesSearch(r.URL.Query().Get("employees_search"))
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		auditFail(r, user, "delete", "employee(id="+idStr+")", "invalid_id")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Некорректный идентификатор сотрудника"
			})
		return
	}
	existing, gerr := getEmployee(a.db, id)
	if gerr != nil {
		if errors.Is(gerr, errEmployeeNotFound) {
			auditFail(r, user, "delete", targetEmployee(id, ""), "not_found")
			a.renderConfigPageWith(w, user,
				groupsDir, groupsPage,
				projectsSort, projectsDir, projectsPage,
				employeesSort, employeesDir, employeesPage,
				employeesGroupID,
				employeesSearch,
				func(d *ConfigPageData) {
					d.PageError = "Сотрудник не найден"
				})
			return
		}
		log.Printf("ошибка чтения сотрудника: %v", gerr)
		auditFailF(r, user, "delete", targetEmployee(id, ""), "db_error: %v", gerr)
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Не удалось удалить сотрудника"
			})
		return
	}
	target := targetEmployee(id, existing.Login)
	deletedEntries, err := deleteEmployee(a.db, id)
	switch {
	case err == nil:
		auditOK(r, user, "delete", target+" work_entries="+strconv.FormatInt(deletedEntries, 10))
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID, employeesSearch,
		), http.StatusSeeOther)
	case errors.Is(err, errEmployeeNotFound):
		auditFail(r, user, "delete", target, "not_found")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Сотрудник не найден"
			})
	default:
		log.Printf("ошибка удаления сотрудника: %v", err)
		auditFailF(r, user, "delete", target, "db_error: %v", err)
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Не удалось удалить сотрудника"
			})
	}
}

func parseOrZero(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func (a *App) generatePasswordHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	length := 8
	if v := strings.TrimSpace(r.URL.Query().Get("length")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			length = n
		}
	}
	if length < 4 {
		length = 4
	}
	if length > 8 {
		length = 8
	}
	password, err := generatePassword(length)
	if err != nil {
		log.Printf("ошибка генерации пароля для пользователя %s: %v", user, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(map[string]string{"password": password}); err != nil {
		log.Printf("ошибка сериализации пароля в JSON: %v", err)
	}
}

func (a *App) nextEmployeeIDHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var nextID int64 = 1
	err := a.db.QueryRow(`SELECT COALESCE(MAX(id), 0) + 1 FROM employees`).Scan(&nextID)
	if err != nil {
		log.Printf("ошибка чтения следующего ID сотрудника: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	_ = user
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(map[string]int64{"next_id": nextID}); err != nil {
		log.Printf("ошибка сериализации next_id в JSON: %v", err)
	}
}