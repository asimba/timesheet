package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
)
const (
	groupsPerPage    = 10
	projectsPerPage  = 10
	employeesPerPage = 10
)
func (a *App) configPageHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	groupsDir := normalizeGroupsDir(r.URL.Query().Get("groups_dir"))
	groupsPage := normalizeGroupsPage(r.URL.Query().Get("groups_page"))
	projectsSort := normalizeProjectsSort(r.URL.Query().Get("projects_sort"))
	projectsDir := normalizeProjectsDir(r.URL.Query().Get("projects_dir"))
	projectsPage := normalizeProjectsPage(r.URL.Query().Get("projects_page"))
	projectsSearch := normalizeProjectsSearch(r.URL.Query().Get("projects_search"))
	employeesSort := normalizeEmployeesSort(r.URL.Query().Get("employees_sort"))
	employeesDir := normalizeEmployeesDir(r.URL.Query().Get("employees_dir"))
	employeesPage := normalizeEmployeesPage(r.URL.Query().Get("employees_page"))
	employeesGroupID := normalizeEmployeesGroupID(r.URL.Query().Get("employees_group_id"))
	employeesSearch := normalizeEmployeesSearch(r.URL.Query().Get("employees_search"))
	switch r.URL.Query().Get("partial") {
	case "groups":
		a.writeGroupsPartial(w, groupsDir, groupsPage)
		return
	case "projects":
		a.writeProjectsPartial(w, projectsSort, projectsDir, projectsPage, projectsSearch)
		return
	case "employees":
		a.writeEmployeesPartial(w, employeesSort, employeesDir, employeesPage, employeesGroupID, employeesSearch)
		return
	}
	a.renderConfigPageWith(w, user,
		groupsDir, groupsPage,
		projectsSort, projectsDir, projectsPage, projectsSearch,
		employeesSort, employeesDir, employeesPage,
		employeesGroupID,
		employeesSearch,
		nil)
}
func paginateGroups(all []Group, page int) (pageItems []Group, totalPages, total int) {
	total = len(all)
	if total == 0 {
		return nil, 0, 0
	}
	totalPages = (total + groupsPerPage - 1) / groupsPerPage
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * groupsPerPage
	end := start + groupsPerPage
	if end > total {
		end = total
	}
	return all[start:end], totalPages, total
}
func paginateProjects(all []Project, page int) (pageItems []Project, totalPages, total int) {
	total = len(all)
	if total == 0 {
		return nil, 0, 0
	}
	totalPages = (total + projectsPerPage - 1) / projectsPerPage
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * projectsPerPage
	end := start + projectsPerPage
	if end > total {
		end = total
	}
	return all[start:end], totalPages, total
}
func paginateEmployees(all []Employee, page int) (pageItems []Employee, totalPages, total int) {
	total = len(all)
	if total == 0 {
		return nil, 0, 0
	}
	totalPages = (total + employeesPerPage - 1) / employeesPerPage
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * employeesPerPage
	end := start + employeesPerPage
	if end > total {
		end = total
	}
	return all[start:end], totalPages, total
}
func effectivePage(page, totalPages int) int {
	if totalPages == 0 {
		return 1
	}
	if page < 1 {
		return 1
	}
	if page > totalPages {
		return totalPages
	}
	return page
}
func (a *App) writeGroupsPartial(w http.ResponseWriter, groupsDir string, groupsPage int) {
	all, err := listGroups(a.db, groupsDir)
	if err != nil {
		log.Printf("ошибка чтения групп: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	items, totalPages, total := paginateGroups(all, groupsPage)
	page := effectivePage(groupsPage, totalPages)
	type groupJSON struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		EmployeeCount int64  `json:"employee_count"`
	}
	resp := struct {
		Groups     []groupJSON `json:"groups"`
		GroupsDir  string      `json:"groups_dir"`
		GroupsPage int         `json:"groups_page"`
		TotalPages int         `json:"groups_total_pages"`
		Total      int         `json:"groups_total"`
	}{
		Groups:     make([]groupJSON, 0, len(items)),
		GroupsDir:  groupsDir,
		GroupsPage: page,
		TotalPages: totalPages,
		Total:      total,
	}
	for _, g := range items {
		resp.Groups = append(resp.Groups, groupJSON{ID: g.ID, Name: g.Name, EmployeeCount: g.EmployeeCount})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("ошибка сериализации групп в JSON: %v", err)
	}
}
func (a *App) writeProjectsPartial(w http.ResponseWriter, projectsSort, projectsDir string, projectsPage int, projectsSearch string) {
	all, err := listProjects(a.db, projectsSort, projectsDir, projectsSearch)
	if err != nil {
		log.Printf("ошибка чтения проектов: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	items, totalPages, total := paginateProjects(all, projectsPage)
	page := effectivePage(projectsPage, totalPages)
	type projectJSON struct {
		ID               int64  `json:"id"`
		Name             string `json:"name"`
		Code             string `json:"code"`
		WorkEntriesCount int64  `json:"work_entries_count"`
	}
	resp := struct {
		Projects       []projectJSON `json:"projects"`
		ProjectsSort   string        `json:"projects_sort"`
		ProjectsDir    string        `json:"projects_dir"`
		ProjectsPage   int           `json:"projects_page"`
		TotalPages     int           `json:"projects_total_pages"`
		Total          int           `json:"projects_total"`
		ProjectsSearch string        `json:"projects_search"`
	}{
		Projects:       make([]projectJSON, 0, len(items)),
		ProjectsSort:   projectsSort,
		ProjectsDir:    projectsDir,
		ProjectsPage:   page,
		TotalPages:     totalPages,
		Total:          total,
		ProjectsSearch: projectsSearch,
	}
	for _, p := range items {
		resp.Projects = append(resp.Projects, projectJSON{
			ID:               p.ID, 
			Name:             p.Name, 
			Code:             p.Code,
			WorkEntriesCount: p.WorkEntriesCount,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("ошибка сериализации проектов в JSON: %v", err)
	}
}
func (a *App) writeEmployeesPartial(w http.ResponseWriter, employeesSort, employeesDir string, employeesPage int, employeesGroupID int64, employeesSearch string) {
	all, err := listEmployees(a.db, employeesSort, employeesDir, employeesGroupID, employeesSearch)
	if err != nil {
		log.Printf("ошибка чтения сотрудников: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	items, totalPages, total := paginateEmployees(all, employeesPage)
	page := effectivePage(employeesPage, totalPages)
	type employeeJSON struct {
		ID               int64  `json:"id"`
		Login            string `json:"login"`
		LastName         string `json:"last_name"`
		FirstName        string `json:"first_name"`
		MiddleName       string `json:"middle_name"`
		GroupID          int64  `json:"group_id"`
		GroupName        string `json:"group_name"`
		WorkEntriesCount int64  `json:"work_entries_count"`
	}
	resp := struct {
		Employees        []employeeJSON `json:"employees"`
		EmployeesSort    string         `json:"employees_sort"`
		EmployeesDir     string         `json:"employees_dir"`
		EmployeesPage    int            `json:"employees_page"`
		TotalPages       int            `json:"employees_total_pages"`
		Total            int            `json:"employees_total"`
		EmployeesGroupID int64          `json:"employees_group_id"`
		EmployeesSearch  string         `json:"employees_search"`
	}{
		Employees:        make([]employeeJSON, 0, len(items)),
		EmployeesSort:    employeesSort,
		EmployeesDir:     employeesDir,
		EmployeesPage:    page,
		TotalPages:       totalPages,
		Total:            total,
		EmployeesGroupID: employeesGroupID,
		EmployeesSearch:  employeesSearch,
	}
	for _, e := range items {
		resp.Employees = append(resp.Employees, employeeJSON{
			ID:               e.ID,
			Login:            e.Login,
			LastName:         e.LastName,
			FirstName:        e.FirstName,
			MiddleName:       e.MiddleName,
			GroupID:          e.GroupID,
			GroupName:        e.GroupName,
			WorkEntriesCount: e.WorkEntriesCount,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("ошибка сериализации сотрудников в JSON: %v", err)
	}
}
func normalizeGroupsDir(dir string) string {
	if dir != "desc" {
		return "asc"
	}
	return "desc"
}
func normalizeGroupsPage(p string) int {
	return normalizePage(p)
}
func normalizeProjectsSort(field string) string {
	if field != "code" {
		return "name"
	}
	return "code"
}
func normalizeProjectsDir(dir string) string {
	if dir != "desc" {
		return "asc"
	}
	return "desc"
}
func normalizeProjectsPage(p string) int {
	return normalizePage(p)
}
func normalizeProjectsSearch(s string) string {
	return strings.TrimSpace(s)
}
func normalizeEmployeesSort(field string) string {
	switch field {
	case "last_name", "group_name":
		return field
	}
	return "login"
}
func normalizeEmployeesDir(dir string) string {
	if dir != "desc" {
		return "asc"
	}
	return "desc"
}
func normalizeEmployeesPage(p string) int {
	return normalizePage(p)
}
func normalizeEmployeesGroupID(p string) int64 {
	if p == "" {
		return 0
	}
	n, err := strconv.ParseInt(p, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
func normalizeEmployeesSearch(s string) string {
	return strings.TrimSpace(s)
}
func normalizePage(p string) int {
	if p == "" {
		return 1
	}
	n := 0
	for _, c := range p {
		if c < '0' || c > '9' {
			return 1
		}
		n = n*10 + int(c-'0')
		if n > 100000 {
			return 1
		}
	}
	if n < 1 {
		return 1
	}
	return n
}
func (a *App) renderConfigPage(w http.ResponseWriter, user, groupErr, employeeErr string) {
	a.renderConfigPageWith(w, user,
		"asc", 1,
		"name", "asc", 1, "",
		"login", "asc", 1,
		0,
		"",
		func(d *ConfigPageData) {
			d.GroupError = groupErr
			d.EmployeeError = employeeErr
		})
}
func (a *App) renderConfigPageWithPageError(w http.ResponseWriter, user, pageErr string) {
	a.renderConfigPageWith(w, user,
		"asc", 1,
		"name", "asc", 1, "",
		"login", "asc", 1,
		0,
		"",
		func(d *ConfigPageData) {
			d.PageError = pageErr
		})
}
func (a *App) renderConfigPageWith(
	w http.ResponseWriter,
	user, groupsDir string, groupsPage int,
	projectsSort, projectsDir string, projectsPage int, projectsSearch string,
	employeesSort, employeesDir string, employeesPage int,
	employeesGroupID int64,
	employeesSearch string,
	mutate func(*ConfigPageData),
) {
	allGroups, err := listGroups(a.db, groupsDir)
	if err != nil {
		log.Printf("ошибка чтения групп: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	groups, groupsTotalPages, groupsTotal := paginateGroups(allGroups, groupsPage)
	groupsPage = effectivePage(groupsPage, groupsTotalPages)
	allProjects, err := listProjects(a.db, projectsSort, projectsDir, projectsSearch)
	if err != nil {
		log.Printf("ошибка чтения проектов: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	projects, projectsTotalPages, projectsTotal := paginateProjects(allProjects, projectsPage)
	projectsPage = effectivePage(projectsPage, projectsTotalPages)
	allEmployees, err := listEmployees(a.db, employeesSort, employeesDir, employeesGroupID, employeesSearch)
	if err != nil {
		log.Printf("ошибка чтения сотрудников: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	employees, employeesTotalPages, employeesTotal := paginateEmployees(allEmployees, employeesPage)
	employeesPage = effectivePage(employeesPage, employeesTotalPages)
	data := &ConfigPageData{
		Username:            user,
		Groups:              groups,
		AllGroups:           allGroups,
		Projects:            projects,
		Employees:           employees,
		GroupsDir:           groupsDir,
		GroupsPage:          groupsPage,
		GroupsTotalPages:    groupsTotalPages,
		GroupsTotal:         groupsTotal,
		ProjectsSort:        projectsSort,
		ProjectsDir:         projectsDir,
		ProjectsPage:        projectsPage,
		ProjectsTotalPages:  projectsTotalPages,
		ProjectsTotal:       projectsTotal,
		ProjectsSearch:      projectsSearch,
		EmployeesSort:       employeesSort,
		EmployeesDir:        employeesDir,
		EmployeesPage:       employeesPage,
		EmployeesTotalPages: employeesTotalPages,
		EmployeesTotal:      employeesTotal,
		EmployeesGroupID:    employeesGroupID,
		EmployeesSearch:     employeesSearch,
	}
	if mutate != nil {
		mutate(data)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.Config.Execute(w, data); err != nil {
		log.Printf("ошибка рендера config: %v", err)
	}
}