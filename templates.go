package main

import (
	"embed"
	"fmt"
	"html/template"
	"time"
)

//go:embed web/login.html web/config.html web/timesheets.html
var templatesFS embed.FS

var baseTemplateFuncs = template.FuncMap{
	"inc": func(n int) int { return n + 1 },
	"globalIndex": func(page, i, perPage int) int {
		if page < 1 {
			page = 1
		}
		if perPage < 1 {
			perPage = 1
		}
		return (page-1)*perPage + i + 1
	},
	"endTime": func(start string, durationMinutes int64) string {
		h, m, ok := parseHHMMToParts(start)
		if !ok {
			return ""
		}
		total := h*60 + m + int(durationMinutes)
		total %= 24 * 60
		return fmt.Sprintf("%02d:%02d", total/60, total%60)
	},
	"durationHHMM": func(minutes int64) string {
		if minutes < 0 {
			minutes = 0
		}
		return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
	},
	"maxWorkDate": func() string {
		return time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	},
	"isWeekend": func(workDate string) bool {
		d, err := time.Parse("2006-01-02", workDate)
		if err != nil {
			return false
		}
		wd := d.Weekday()
		return wd == time.Saturday || wd == time.Sunday
	},
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	"pagesRange": func(total int) []int {
		if total < 1 {
			return nil
		}
		out := make([]int, 0, total)
		for i := 1; i <= total; i++ {
			out = append(out, i)
		}
		return out
	},
}

func parseHHMMToParts(s string) (int, int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

type Templates struct {
	Login      *template.Template
	Config     *template.Template
	Timesheets *template.Template
}

func buildTemplates(a *App) (*Templates, error) {
	funcs := template.FuncMap{
		"isEditableWorkDate": a.isEditableWorkDate,
		"minWorkDate":        a.minWorkDate,
	}
	for k, v := range baseTemplateFuncs {
		funcs[k] = v
	}
	login, err := template.New("login.html").Funcs(funcs).ParseFS(templatesFS, "web/login.html")
	if err != nil {
		return nil, err
	}
	config, err := template.New("config.html").Funcs(funcs).ParseFS(templatesFS, "web/config.html")
	if err != nil {
		return nil, err
	}
	timesheets, err := template.New("timesheets.html").Funcs(funcs).ParseFS(templatesFS, "web/timesheets.html")
	if err != nil {
		return nil, err
	}
	return &Templates{Login: login, Config: config, Timesheets: timesheets}, nil
}

type LoginData struct {
	Error               string
	WorkEntriesEditDays int
}

type ConfigPageData struct {
	Username  string
	Groups    []Group
	AllGroups []Group
	Projects  []Project
	Employees []Employee

	GroupsDir     string
	ProjectsSort  string
	ProjectsDir   string
	EmployeesSort string
	EmployeesDir  string

	GroupsPage       int
	GroupsTotalPages int
	GroupsTotal      int

	ProjectsPage       int
	ProjectsTotalPages int
	ProjectsTotal      int
	ProjectsSearch     string

	EmployeesPage       int
	EmployeesTotalPages int
	EmployeesTotal      int
	EmployeesGroupID    int64
	EmployeesSearch     string

	ActiveSessions map[int64]int

	PageError string

	GroupError    string
	EmployeeError string
	ProjectError  string

	EditEmployee *Employee
	EditError    string

	AddLogin      string
	AddLastName   string
	AddFirstName  string
	AddMiddleName string
	AddGroupID    int64

	EditGroup      *Group
	EditGroupError string

	AddProjectName string
	AddProjectCode string

	EditProject      *Project
	EditProjectError string
}

type TimesheetsData struct {
	Username   string
	Role       Role
	EmployeeID int64
	FullName   string
	GroupName  string

	ViewedEmployeeID int64
	ViewedLogin      string

	Projects    []Project
	WorkEntries []WorkEntry
	WorkError   string

	WorksMonth      string
	WorksMonthLabel string
	WorksPrevMonth  string
	WorksNextMonth  string
	WorksCanPrev    bool
	WorksCanNext    bool

	AddWorkDate      string
	AddWorkStart     string
	AddWorkEnd       string
	AddWorkDurationH string
	AddWorkDurationM string
	AddWorkProjectID int64
	AddWorkComment   string
	AddWorkMode      string

	EditWork          *WorkEntry
	EditWorkError     string
	EditWorkDurationH string
	EditWorkDurationM string
	EditWorkMode      string

	ExportMode string
}