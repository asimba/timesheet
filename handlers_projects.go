package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func (a *App) addProjectHandler(w http.ResponseWriter, r *http.Request) {
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
	name := strings.TrimSpace(r.FormValue("name"))
	code := strings.TrimSpace(r.FormValue("code"))
	target := targetProject(0, name, code)
	reRenderWithError := func(errMsg string) {
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.ProjectError = errMsg
				d.AddProjectName = name
				d.AddProjectCode = code
			})
	}
	if name == "" {
		auditFail(r, user, "create", target, "empty_name")
		reRenderWithError("Наименование не может быть пустым")
		return
	}
	if len([]rune(name)) > 1024 {
		auditFail(r, user, "create", target, "name_too_long")
		reRenderWithError("Наименование слишком длинное (макс. 1024 символа)")
		return
	}
	if code == "" {
		auditFail(r, user, "create", target, "empty_code")
		reRenderWithError("Кодовое обозначение обязательно")
		return
	}
	if len([]rune(code)) > 20 {
		auditFail(r, user, "create", target, "code_too_long")
		reRenderWithError("Кодовое обозначение слишком длинное (макс. 20 символов)")
		return
	}
	id, err := addProject(a.db, Project{Name: name, Code: code})
	switch {
	case err == nil:
		auditOK(r, user, "create", targetProject(id, name, code))
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
		), http.StatusSeeOther)
	case errors.Is(err, errDuplicateProjectCode):
		auditFail(r, user, "create", target, "duplicate_code")
		reRenderWithError("Проект с таким кодовым обозначением уже существует")
	default:
		log.Printf("ошибка вставки проекта: %v", err)
		auditFailF(r, user, "create", target, "db_error: %v", err)
		reRenderWithError("Не удалось сохранить проект")
	}
}

func (a *App) editProjectHandler(w http.ResponseWriter, r *http.Request) {
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
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		auditFail(r, user, "update", "project(id="+idStr+")", "invalid_id")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.PageError = "Некорректный идентификатор проекта"
			})
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	code := strings.TrimSpace(r.FormValue("code"))
	target := targetProject(id, name, code)
	reRenderWithEdit := func(errMsg string) {
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.EditProject = &Project{ID: id, Name: name, Code: code}
				d.EditProjectError = errMsg
			})
	}
	if name == "" {
		auditFail(r, user, "update", target, "empty_name")
		reRenderWithEdit("Наименование не может быть пустым")
		return
	}
	if len([]rune(name)) > 1024 {
		auditFail(r, user, "update", target, "name_too_long")
		reRenderWithEdit("Наименование слишком длинное (макс. 1024 символа)")
		return
	}
	if code == "" {
		auditFail(r, user, "update", target, "empty_code")
		reRenderWithEdit("Кодовое обозначение обязательно")
		return
	}
	if len([]rune(code)) > 20 {
		auditFail(r, user, "update", target, "code_too_long")
		reRenderWithEdit("Кодовое обозначение слишком длинное (макс. 20 символов)")
		return
	}
	err = updateProject(a.db, Project{ID: id, Name: name, Code: code})
	switch {
	case err == nil:
		auditOK(r, user, "update", target)
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
		), http.StatusSeeOther)
	case errors.Is(err, errDuplicateProjectCode):
		auditFail(r, user, "update", target, "duplicate_code")
		reRenderWithEdit("Проект с таким кодовым обозначением уже существует")
	case errors.Is(err, errProjectNotFound):
		auditFail(r, user, "update", target, "not_found")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.PageError = "Проект не найден"
			})
	default:
		log.Printf("ошибка обновления проекта: %v", err)
		auditFailF(r, user, "update", target, "db_error: %v", err)
		reRenderWithEdit("Не удалось сохранить изменения")
	}
}

func (a *App) deleteProjectHandler(w http.ResponseWriter, r *http.Request) {
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
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		auditFail(r, user, "delete", "project(id="+idStr+")", "invalid_id")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.PageError = "Некорректный идентификатор проекта"
			})
		return
	}
	p, err := getProject(a.db, id)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			auditFail(r, user, "delete", targetProject(id, "", ""), "not_found")
			a.renderConfigPageWith(w, user,
				groupsDir, groupsPage,
				projectsSort, projectsDir, projectsPage,
				employeesSort, employeesDir, employeesPage,
				employeesGroupID,
				func(d *ConfigPageData) {
					d.PageError = "Проект не найден"
				})
			return
		}
		log.Printf("ошибка чтения проекта: %v", err)
		auditFailF(r, user, "delete", targetProject(id, "", ""), "db_error: %v", err)
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.PageError = "Не удалось удалить проект"
			})
		return
	}
	target := targetProject(p.ID, p.Name, p.Code)
	err = deleteProject(a.db, id)
	switch {
	case err == nil:
		auditOK(r, user, "delete", target)
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
		), http.StatusSeeOther)
	case errors.Is(err, errProjectHasWorkEntries):
		auditFail(r, user, "delete", target, "has_work_entries")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.PageError = "Нельзя удалить проект: на него ссылаются записи табеля"
			})
	case errors.Is(err, errProjectNotFound):
		auditFail(r, user, "delete", target, "not_found")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.PageError = "Проект не найден"
			})
	default:
		log.Printf("ошибка удаления проекта: %v", err)
		auditFailF(r, user, "delete", target, "db_error: %v", err)
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			func(d *ConfigPageData) {
				d.PageError = "Не удалось удалить проект"
			})
	}
}