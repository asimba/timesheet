package main

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func configParamsFromQuery(r *http.Request) (
	groupsDir string, groupsPage int,
	projectsSort, projectsDir string, projectsPage int,
	employeesSort, employeesDir string, employeesPage int,
	employeesGroupID int64,
) {
	return normalizeGroupsDir(r.URL.Query().Get("groups_dir")),
		normalizeGroupsPage(r.URL.Query().Get("groups_page")),
		normalizeProjectsSort(r.URL.Query().Get("projects_sort")),
		normalizeProjectsDir(r.URL.Query().Get("projects_dir")),
		normalizeProjectsPage(r.URL.Query().Get("projects_page")),
		normalizeEmployeesSort(r.URL.Query().Get("employees_sort")),
		normalizeEmployeesDir(r.URL.Query().Get("employees_dir")),
		normalizeEmployeesPage(r.URL.Query().Get("employees_page")),
		normalizeEmployeesGroupID(r.URL.Query().Get("employees_group_id"))
}

func configRedirectURL(
	groupsDir string, groupsPage int,
	projectsSort, projectsDir string, projectsPage int,
	employeesSort, employeesDir string, employeesPage int,
	employeesGroupID int64,
	employeesSearch string,
) string {
	return "/config?groups_dir=" + groupsDir +
		"&groups_page=" + strconv.Itoa(groupsPage) +
		"&projects_sort=" + projectsSort +
		"&projects_dir=" + projectsDir +
		"&projects_page=" + strconv.Itoa(projectsPage) +
		"&employees_sort=" + employeesSort +
		"&employees_dir=" + employeesDir +
		"&employees_page=" + strconv.Itoa(employeesPage) +
		"&employees_group_id=" + strconv.FormatInt(employeesGroupID, 10) +
		"&employees_search=" + url.QueryEscape(employeesSearch)
}

func (a *App) addGroupHandler(w http.ResponseWriter, r *http.Request) {
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
	name := strings.TrimSpace(r.FormValue("name"))
	target := targetGroup(0, name)
	reRenderWithError := func(errMsg string) {
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.GroupError = errMsg
			})
	}
	if name == "" {
		auditFail(r, user, "create", target, "empty_name")
		reRenderWithError("Наименование не может быть пустым")
		return
	}
	if len([]rune(name)) > 200 {
		auditFail(r, user, "create", target, "name_too_long")
		reRenderWithError("Наименование слишком длинное (макс. 200 символов)")
		return
	}
	id, err := addGroup(a.db, name)
	switch {
	case err == nil:
		auditOK(r, user, "create", targetGroup(id, name))
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID, employeesSearch,
		), http.StatusSeeOther)
	case errors.Is(err, errDuplicateGroup):
		auditFail(r, user, "create", target, "duplicate_name")
		reRenderWithError("Группа с таким наименованием уже существует")
	default:
		log.Printf("ошибка вставки группы: %v", err)
		auditFailF(r, user, "create", target, "db_error: %v", err)
		reRenderWithError("Не удалось сохранить группу")
	}
}

func (a *App) editGroupHandler(w http.ResponseWriter, r *http.Request) {
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
		auditFail(r, user, "update", "group(id="+idStr+")", "invalid_id")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Некорректный идентификатор группы"
			})
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	target := targetGroup(id, name)
	reRenderWithEdit := func(errMsg string) {
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.EditGroup = &Group{ID: id, Name: name}
				d.EditGroupError = errMsg
			})
	}
	if name == "" {
		auditFail(r, user, "update", target, "empty_name")
		reRenderWithEdit("Наименование не может быть пустым")
		return
	}
	if len([]rune(name)) > 200 {
		auditFail(r, user, "update", target, "name_too_long")
		reRenderWithEdit("Наименование слишком длинное (макс. 200 символов)")
		return
	}
	err = updateGroup(a.db, Group{ID: id, Name: name})
	switch {
	case err == nil:
		auditOK(r, user, "update", target)
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID, employeesSearch,
		), http.StatusSeeOther)
	case errors.Is(err, errDuplicateGroup):
		auditFail(r, user, "update", target, "duplicate_name")
		reRenderWithEdit("Группа с таким наименованием уже существует")
	case errors.Is(err, errGroupNotFound):
		auditFail(r, user, "update", target, "not_found")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Группа не найдена"
			})
	default:
		log.Printf("ошибка обновления группы: %v", err)
		auditFailF(r, user, "update", target, "db_error: %v", err)
		reRenderWithEdit("Не удалось сохранить изменения")
	}
}

func (a *App) deleteGroupHandler(w http.ResponseWriter, r *http.Request) {
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
		auditFail(r, user, "delete", "group(id="+idStr+")", "invalid_id")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Некорректный идентификатор группы"
			})
		return
	}
	g, err := getGroup(a.db, id)
	if err != nil {
		if errors.Is(err, errGroupNotFound) {
			auditFail(r, user, "delete", targetGroup(id, ""), "not_found")
			a.renderConfigPageWith(w, user,
				groupsDir, groupsPage,
				projectsSort, projectsDir, projectsPage,
				employeesSort, employeesDir, employeesPage,
				employeesGroupID,
				employeesSearch,
				func(d *ConfigPageData) {
					d.PageError = "Группа не найдена"
				})
			return
		}
		log.Printf("ошибка чтения группы: %v", err)
		auditFailF(r, user, "delete", targetGroup(id, ""), "db_error: %v", err)
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Не удалось удалить группу"
			})
		return
	}
	target := targetGroup(g.ID, g.Name)
	err = deleteGroup(a.db, id)
	switch {
	case err == nil:
		auditOK(r, user, "delete", target)
		http.Redirect(w, r, configRedirectURL(
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID, employeesSearch,
		), http.StatusSeeOther)
	case errors.Is(err, errGroupHasEmployees):
		auditFail(r, user, "delete", target, "has_employees")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Нельзя удалить отдел: к нему прикреплены сотрудники"
			})
	case errors.Is(err, errGroupNotFound):
		auditFail(r, user, "delete", target, "not_found")
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Группа не найдена"
			})
	default:
		log.Printf("ошибка удаления группы: %v", err)
		auditFailF(r, user, "delete", target, "db_error: %v", err)
		a.renderConfigPageWith(w, user,
			groupsDir, groupsPage,
			projectsSort, projectsDir, projectsPage,
			employeesSort, employeesDir, employeesPage,
			employeesGroupID,
			employeesSearch,
			func(d *ConfigPageData) {
				d.PageError = "Не удалось удалить группу"
			})
	}
}