package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var monthsRU = []string{
	"", "Январь", "Февраль", "Март", "Апрель", "Май", "Июнь",
	"Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь",
}

func (a *App) timesheetsHandler(w http.ResponseWriter, r *http.Request) {
	s, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.URL.Query().Get("partial") == "works" {
		worksMonth := normalizeWorksMonth(r.URL.Query().Get("works_month"))
		a.writeWorksPartial(w, r, s, worksMonth)
		return
	}
	data := TimesheetsData{
		Username:    s.User,
		Role:        s.Role,
		EmployeeID:  s.EmployeeID,
		AddWorkMode: "end",
	}
	worksMonthRaw := strings.TrimSpace(r.URL.Query().Get("works_month"))
	if s.Role == RoleEmployee {
		if s.EmployeeID > 0 {
			if emp, err := getEmployee(a.db, s.EmployeeID); err == nil {
				data.FullName = formatFullName(emp.LastName, emp.FirstName, emp.MiddleName)
				data.GroupName = emp.GroupName
				data.ViewedEmployeeID = emp.ID
				data.ViewedLogin = emp.Login
			} else {
				log.Printf("ошибка чтения сотрудника для /timesheets: %v", err)
			}
		}
		month := a.resolveWorksMonth(data.ViewedEmployeeID, worksMonthRaw)
		a.fillTimesheetsMonthData(&data, s.User, month)
		a.renderTimesheets(w, data)
		return
	}
	if s.Role == RoleAdmin {
		idStr := r.URL.Query().Get("employee_id")
		if idStr != "" {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 {
				auditFail(r, s.User, "view_timesheets", "employee(id="+idStr+")", "invalid_id")
			} else {
				emp, err := getEmployee(a.db, id)
				if err != nil {
					if errors.Is(err, errEmployeeNotFound) {
						auditFail(r, s.User, "view_timesheets", targetEmployee(id, ""), "not_found")
					} else {
						log.Printf("ошибка чтения сотрудника для /timesheets: %v", err)
						auditFailF(r, s.User, "view_timesheets", targetEmployee(id, ""), "db_error: %v", err)
					}
				} else {
					data.FullName = formatFullName(emp.LastName, emp.FirstName, emp.MiddleName)
					data.GroupName = emp.GroupName
					data.ViewedEmployeeID = emp.ID
					data.ViewedLogin = emp.Login
					auditOK(r, s.User, "view_timesheets", targetEmployee(emp.ID, emp.Login))
				}
			}
		}
		month := a.resolveWorksMonth(data.ViewedEmployeeID, worksMonthRaw)
		a.fillTimesheetsMonthData(&data, s.User, month)
		a.renderTimesheets(w, data)
		return
	}
	http.Error(w, "Forbidden", http.StatusForbidden)
}

func (a *App) resolveWorksMonth(employeeID int64, raw string) string {
	if raw != "" {
		return normalizeWorksMonth(raw)
	}
	if employeeID > 0 {
		if months, err := listWorkEntryMonths(a.db, employeeID); err == nil && len(months) > 0 {
			return months[len(months)-1]
		}
	}
	return time.Now().Format("2006-01")
}

func normalizeWorksMonth(s string) string {
	if len(s) == 7 && s[4] == '-' {
		y, err1 := strconv.Atoi(s[0:4])
		m, err2 := strconv.Atoi(s[5:7])
		if err1 == nil && err2 == nil && m >= 1 && m <= 12 && y >= 1970 && y <= 9999 {
			return s
		}
	}
	return time.Now().Format("2006-01")
}

func monthParts(month string) (year, mon int, ok bool) {
	if len(month) != 7 || month[4] != '-' {
		return 0, 0, false
	}
	y, err1 := strconv.Atoi(month[0:4])
	m, err2 := strconv.Atoi(month[5:7])
	if err1 != nil || err2 != nil || m < 1 || m > 12 {
		return 0, 0, false
	}
	return y, m, true
}

func monthLabel(month string) string {
	y, m, ok := monthParts(month)
	if !ok {
		return month
	}
	return monthsRU[m] + " " + strconv.Itoa(y)
}

func (a *App) fillTimesheetsMonthData(data *TimesheetsData, actor, month string) {
	projects, err := listProjects(a.db, "name", "asc")
	if err != nil {
		log.Printf("ошибка чтения проектов: %v", err)
		return
	}
	data.Projects = projects
	data.WorksMonth = month
	data.WorksMonthLabel = monthLabel(month)
	data.WorksCanPrev = false
	data.WorksCanNext = false
	data.WorksPrevMonth = ""
	data.WorksNextMonth = ""
	if data.ViewedEmployeeID <= 0 {
		_ = actor
		return
	}
	months, err := listWorkEntryMonths(a.db, data.ViewedEmployeeID)
	if err != nil {
		log.Printf("ошибка чтения списка месяцев табеля: %v", err)
		return
	}
	for i := len(months) - 1; i >= 0; i-- {
		if months[i] < month {
			data.WorksPrevMonth = months[i]
			data.WorksCanPrev = true
			break
		}
	}
	for i := 0; i < len(months); i++ {
		if months[i] > month {
			data.WorksNextMonth = months[i]
			data.WorksCanNext = true
			break
		}
	}
	entries, err := listWorkEntriesByMonth(a.db, data.ViewedEmployeeID, month)
	if err != nil {
		log.Printf("ошибка чтения записей табеля за %s: %v", month, err)
		return
	}
	data.WorkEntries = entries
	_ = actor
}

func (a *App) writeWorksPartial(w http.ResponseWriter, r *http.Request, s Session, worksMonth string) {
	employeeID := s.EmployeeID
	if s.Role == RoleAdmin {
		idStr := strings.TrimSpace(r.URL.Query().Get("employee_id"))
		if idStr != "" {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err == nil && id > 0 {
				employeeID = id
			}
		}
	}
	if employeeID <= 0 {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	entries, err := listWorkEntriesByMonth(a.db, employeeID, worksMonth)
	if err != nil {
		log.Printf("ошибка чтения записей табеля: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	months, err := listWorkEntryMonths(a.db, employeeID)
	if err != nil {
		log.Printf("ошибка чтения списка месяцев табеля: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	prevMonth := ""
	canPrev := false
	nextMonth := ""
	canNext := false
	for i := len(months) - 1; i >= 0; i-- {
		if months[i] < worksMonth {
			prevMonth = months[i]
			canPrev = true
			break
		}
	}
	for i := 0; i < len(months); i++ {
		if months[i] > worksMonth {
			nextMonth = months[i]
			canNext = true
			break
		}
	}
	type workJSON struct {
		ID              int64  `json:"id"`
		EmployeeID      int64  `json:"employee_id"`
		EmployeeLogin   string `json:"employee_login"`
		ProjectID       int64  `json:"project_id"`
		ProjectName     string `json:"project_name"`
		ProjectCode     string `json:"project_code"`
		WorkDate        string `json:"work_date"`
		StartTime       string `json:"start_time"`
		DurationMinutes int64  `json:"duration_minutes"`
		Comment         string `json:"comment"`
	}
	resp := struct {
		Works      []workJSON `json:"works"`
		Month      string     `json:"works_month"`
		MonthLabel string     `json:"works_month_label"`
		PrevMonth  string     `json:"works_prev"`
		NextMonth  string     `json:"works_next"`
		CanPrev    bool       `json:"works_can_prev"`
		CanNext    bool       `json:"works_can_next"`
		EmployeeID int64      `json:"employee_id"`
	}{
		Works:      make([]workJSON, 0, len(entries)),
		Month:      worksMonth,
		MonthLabel: monthLabel(worksMonth),
		PrevMonth:  prevMonth,
		NextMonth:  nextMonth,
		CanPrev:    canPrev,
		CanNext:    canNext,
		EmployeeID: employeeID,
	}
	for _, e := range entries {
		resp.Works = append(resp.Works, workJSON{
			ID: e.ID, EmployeeID: e.EmployeeID, EmployeeLogin: e.EmployeeLogin,
			ProjectID: e.ProjectID, ProjectName: e.ProjectName, ProjectCode: e.ProjectCode,
			WorkDate: e.WorkDate, StartTime: e.StartTime,
			DurationMinutes: e.DurationMinutes, Comment: e.Comment,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("ошибка сериализации записей табеля в JSON: %v", err)
	}
}

func (a *App) addWorkEntryHandler(w http.ResponseWriter, r *http.Request) {
	s, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	employeeID := s.EmployeeID
	if s.Role == RoleAdmin {
		idStr := strings.TrimSpace(r.FormValue("employee_id"))
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			a.auditFailWorkEntry(r, s.User, "create", "work_entry(employee_id="+idStr+")", "invalid_employee_id")
			a.renderTimesheetsError(w, s, "Некорректный идентификатор сотрудника")
			return
		}
		employeeID = id
	}
	if employeeID <= 0 {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	workDate := strings.TrimSpace(r.FormValue("work_date"))
	startTime := strings.TrimSpace(r.FormValue("start_time"))
	mode := r.FormValue("mode")
	endTime := strings.TrimSpace(r.FormValue("end_time"))
	durHStr := strings.TrimSpace(r.FormValue("duration_h"))
	durMStr := strings.TrimSpace(r.FormValue("duration_m"))
	projectIDStr := strings.TrimSpace(r.FormValue("project_id"))
	comment := strings.TrimSpace(r.FormValue("comment"))
	if mode != "end" && mode != "duration" {
		mode = "end"
	}
	prefill := func() TimesheetsData {
		data := TimesheetsData{
			Username:         s.User,
			Role:             s.Role,
			EmployeeID:       s.EmployeeID,
			AddWorkDate:      workDate,
			AddWorkStart:     startTime,
			AddWorkEnd:       endTime,
			AddWorkDurationH: durHStr,
			AddWorkDurationM: durMStr,
			AddWorkProjectID: parseOrZero(projectIDStr),
			AddWorkComment:   comment,
			AddWorkMode:      mode,
		}
		targetID := employeeID
		if s.Role == RoleEmployee {
			targetID = s.EmployeeID
		}
		if targetID > 0 {
			if emp, err := getEmployee(a.db, targetID); err == nil {
				data.FullName = formatFullName(emp.LastName, emp.FirstName, emp.MiddleName)
				data.GroupName = emp.GroupName
				data.ViewedEmployeeID = emp.ID
				data.ViewedLogin = emp.Login
			}
		}
		month := ""
		if workDate != "" {
			month = workDate[:7]
		}
		a.fillTimesheetsMonthData(&data, s.User, normalizeWorksMonth(month))
		return data
	}
	reRenderWithError := func(msg string) {
		data := prefill()
		data.WorkError = msg
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := a.tmpl.Timesheets.Execute(w, data); err != nil {
			log.Printf("ошибка рендера timesheets: %v", err)
		}
	}
	if workDate == "" {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "empty_date")
		reRenderWithError("Укажите дату работ")
		return
	}
	if !isWorkDateAllowed(workDate) {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "date_in_future")
		reRenderWithError("Дата работ не может быть позже, чем на один день от текущей")
		return
	}
	if !a.isEditableWorkDate(s.Role, workDate) {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "date_too_old")
		reRenderWithError("Дата работ не может быть старше установленного срока (" +
			strconv.Itoa(a.cfg.WorkEntriesEditDays) + " дн.)")
		return
	}
	if startTime == "" {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "empty_start_time")
		reRenderWithError("Укажите время начала работ")
		return
	}
	if projectIDStr == "" {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "empty_project")
		reRenderWithError("Выберите объект/проект")
		return
	}
	if comment == "" {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "empty_comment")
		reRenderWithError("Укажите вид работ")
		return
	}
	if len([]rune(comment)) > 512 {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "comment_too_long")
		reRenderWithError("Вид работ не должен превышать 512 символов")
		return
	}
	projectID, err := strconv.ParseInt(projectIDStr, 10, 64)
	if err != nil || projectID <= 0 {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "invalid_project_id")
		reRenderWithError("Некорректный объект/проект")
		return
	}
	if _, err := getProject(a.db, projectID); err != nil {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "project_not_found")
		reRenderWithError("Выбранный объект/проект не найден")
		return
	}
	if _, err := getEmployee(a.db, employeeID); err != nil {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "employee_not_found")
		reRenderWithError("Сотрудник не найден")
		return
	}
	last, err := getLastWorkEntry(a.db, employeeID)
	if err != nil {
		log.Printf("ошибка чтения последней записи табеля: %v", err)
		a.auditFailWorkEntryF(r, s.User, "create", "work_entry", "db_error: %v", err)
		reRenderWithError("Не удалось проверить предыдущие записи")
		return
	}
	if last != nil {
		if workDate < last.WorkDate ||
			(workDate == last.WorkDate && startTime < last.StartTime) {
			a.auditFailWorkEntry(r, s.User, "create",
				"work_entry(employee_id="+strconv.FormatInt(employeeID, 10)+
					", date="+workDate+", start="+startTime+")",
				"before_previous")
			reRenderWithError("Дата и время новой записи не могут быть раньше предыдущей записи (" +
				last.WorkDate + " " + last.StartTime + ")")
			return
		}
	}
	durationMinutes, errMsg := computeDurationMinutes(startTime, mode, endTime, durHStr, durMStr)
	if errMsg != "" {
		a.auditFailWorkEntry(r, s.User, "create", "work_entry", "invalid_duration")
		reRenderWithError(errMsg)
		return
	}
	entry := WorkEntry{
		EmployeeID:      employeeID,
		ProjectID:       projectID,
		WorkDate:        workDate,
		StartTime:       startTime,
		DurationMinutes: durationMinutes,
		Comment:         comment,
	}
	id, err := addWorkEntry(a.db, entry)
	if err != nil {
		if errors.Is(err, errWorkEntryInvalidRef) {
			a.auditFailWorkEntry(r, s.User, "create", "work_entry", "invalid_ref")
			reRenderWithError("Некорректная ссылка на сотрудника или проект")
			return
		}
		log.Printf("ошибка вставки записи табеля: %v", err)
		a.auditFailWorkEntryF(r, s.User, "create", "work_entry", "db_error: %v", err)
		reRenderWithError("Не удалось сохранить запись")
		return
	}
	a.auditOKWorkEntry(r, s.User, "create", id, employeeID, projectID, workDate, startTime, durationMinutes)
	month := normalizeWorksMonth(workDate[:7])
	redirect := "/timesheets?works_month=" + month
	if s.Role == RoleAdmin {
		redirect = "/timesheets?employee_id=" + strconv.FormatInt(employeeID, 10) + "&works_month=" + month
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *App) editWorkEntryHandler(w http.ResponseWriter, r *http.Request) {
	s, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		a.renderTimesheetsError(w, s, "Некорректный идентификатор записи")
		return
	}
	existing, err := getWorkEntry(a.db, id)
	if err != nil {
		if errors.Is(err, errWorkEntryNotFound) {
			a.renderTimesheetsError(w, s, "Запись табеля не найдена")
			return
		}
		log.Printf("ошибка чтения записи табеля: %v", err)
		a.renderTimesheetsError(w, s, "Не удалось сохранить изменения")
		return
	}
	if s.Role == RoleEmployee && existing.EmployeeID != s.EmployeeID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if !a.isEditableWorkDate(s.Role, existing.WorkDate) {
		a.auditFailWorkEntry(r, s.User, "update", targetWorkEntry(existing), "record_locked")
		a.renderTimesheetsError(w, s, "Запись старше установленного срока нельзя редактировать")
		return
	}
	workDate := strings.TrimSpace(r.FormValue("work_date"))
	startTime := strings.TrimSpace(r.FormValue("start_time"))
	mode := r.FormValue("mode")
	endTime := strings.TrimSpace(r.FormValue("end_time"))
	durHStr := strings.TrimSpace(r.FormValue("duration_h"))
	durMStr := strings.TrimSpace(r.FormValue("duration_m"))
	projectIDStr := strings.TrimSpace(r.FormValue("project_id"))
	comment := strings.TrimSpace(r.FormValue("comment"))
	if mode != "end" && mode != "duration" {
		mode = "end"
	}
	prefillEdit := func() TimesheetsData {
		data := TimesheetsData{
			Username:   s.User,
			Role:       s.Role,
			EmployeeID: s.EmployeeID,
			EditWork: &WorkEntry{
				ID:         id,
				EmployeeID: existing.EmployeeID,
				ProjectID:  parseOrZero(projectIDStr),
				WorkDate:   workDate,
				StartTime:  startTime,
				Comment:    comment,
			},
			EditWorkDurationH: durHStr,
			EditWorkDurationM: durMStr,
			EditWorkMode:      mode,
		}
		targetID := existing.EmployeeID
		if s.Role == RoleEmployee {
			targetID = s.EmployeeID
		}
		if targetID > 0 {
			if emp, err := getEmployee(a.db, targetID); err == nil {
				data.FullName = formatFullName(emp.LastName, emp.FirstName, emp.MiddleName)
				data.GroupName = emp.GroupName
				data.ViewedEmployeeID = emp.ID
				data.ViewedLogin = emp.Login
			}
		}
		month := existing.WorkDate[:7]
		if workDate != "" {
			month = workDate[:7]
		}
		a.fillTimesheetsMonthData(&data, s.User, normalizeWorksMonth(month))
		return data
	}
	reRenderWithError := func(msg string) {
		data := prefillEdit()
		data.EditWorkError = msg
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := a.tmpl.Timesheets.Execute(w, data); err != nil {
			log.Printf("ошибка рендера timesheets (edit): %v", err)
		}
	}
	if workDate == "" {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "empty_date")
		reRenderWithError("Укажите дату работ")
		return
	}
	if !isWorkDateAllowed(workDate) {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "date_in_future")
		reRenderWithError("Дата работ не может быть позже, чем на один день от текущей")
		return
	}
	if startTime == "" {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "empty_start_time")
		reRenderWithError("Укажите время начала работ")
		return
	}
	if projectIDStr == "" {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "empty_project")
		reRenderWithError("Выберите объект/проект")
		return
	}
	if comment == "" {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "empty_comment")
		reRenderWithError("Укажите вид работ")
		return
	}
	if len([]rune(comment)) > 512 {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "comment_too_long")
		reRenderWithError("Вид работ не должен превышать 512 символов")
		return
	}
	projectID, err := strconv.ParseInt(projectIDStr, 10, 64)
	if err != nil || projectID <= 0 {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "invalid_project_id")
		reRenderWithError("Некорректный объект/проект")
		return
	}
	if _, err := getProject(a.db, projectID); err != nil {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "project_not_found")
		reRenderWithError("Выбранный объект/проект не найден")
		return
	}
	prev, next, err := getPrevAndNextWorkEntries(a.db, existing.EmployeeID, id, existing.WorkDate, existing.StartTime)
	if err != nil {
		log.Printf("ошибка чтения соседних записей табеля: %v", err)
		a.auditFailWorkEntryF(r, s.User, "update", "work_entry(id="+idStr+")", "db_error: %v", err)
		reRenderWithError("Не удалось проверить соседние записи")
		return
	}
	newKey := workDate + " " + startTime
	if prev != nil {
		prevKey := prev.WorkDate + " " + prev.StartTime
		if newKey <= prevKey {
			a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "before_previous")
			reRenderWithError("Дата и время записи должны быть позже предыдущей записи (" +
				prev.WorkDate + " " + prev.StartTime + ")")
			return
		}
	}
	if next != nil {
		nextKey := next.WorkDate + " " + next.StartTime
		if newKey >= nextKey {
			a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "after_next")
			reRenderWithError("Дата и время записи должны быть раньше следующей записи (" +
				next.WorkDate + " " + next.StartTime + ")")
			return
		}
	}
	durationMinutes, errMsg := computeDurationMinutes(startTime, mode, endTime, durHStr, durMStr)
	if errMsg != "" {
		a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "invalid_duration")
		reRenderWithError(errMsg)
		return
	}
	entry := WorkEntry{
		ID:              id,
		EmployeeID:      existing.EmployeeID,
		ProjectID:       projectID,
		WorkDate:        workDate,
		StartTime:       startTime,
		DurationMinutes: durationMinutes,
		Comment:         comment,
	}
	err = updateWorkEntry(a.db, entry)
	if err != nil {
		if errors.Is(err, errWorkEntryInvalidRef) {
			a.auditFailWorkEntry(r, s.User, "update", "work_entry(id="+idStr+")", "invalid_ref")
			reRenderWithError("Некорректная ссылка на проект")
			return
		}
		if errors.Is(err, errWorkEntryNotFound) {
			a.renderTimesheetsError(w, s, "Запись табеля не найдена")
			return
		}
		log.Printf("ошибка обновления записи табеля: %v", err)
		a.auditFailWorkEntryF(r, s.User, "update", "work_entry(id="+idStr+")", "db_error: %v", err)
		reRenderWithError("Не удалось сохранить изменения")
		return
	}
	a.auditOKWorkEntry(r, s.User, "update", id, entry.EmployeeID, projectID, workDate, startTime, durationMinutes)
	month := normalizeWorksMonth(workDate[:7])
	redirect := "/timesheets?works_month=" + month
	if s.Role == RoleAdmin {
		redirect = "/timesheets?employee_id=" + strconv.FormatInt(entry.EmployeeID, 10) + "&works_month=" + month
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *App) deleteWorkEntryHandler(w http.ResponseWriter, r *http.Request) {
	s, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		a.renderTimesheetsError(w, s, "Некорректный идентификатор записи")
		return
	}
	existing, err := getWorkEntry(a.db, id)
	if err != nil {
		if errors.Is(err, errWorkEntryNotFound) {
			a.renderTimesheetsError(w, s, "Запись табеля не найдена")
			return
		}
		log.Printf("ошибка чтения записи табеля: %v", err)
		a.renderTimesheetsError(w, s, "Не удалось удалить запись")
		return
	}
	if s.Role == RoleEmployee && existing.EmployeeID != s.EmployeeID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if !a.isEditableWorkDate(s.Role, existing.WorkDate) {
		a.auditFailWorkEntry(r, s.User, "delete", targetWorkEntry(existing), "record_locked")
		a.renderTimesheetsError(w, s, "Запись старше установленного срока нельзя удалить")
		return
	}
	err = deleteWorkEntry(a.db, id)
	if err != nil {
		if errors.Is(err, errWorkEntryNotFound) {
			a.renderTimesheetsError(w, s, "Запись табеля не найдена")
			return
		}
		log.Printf("ошибка удаления записи табеля: %v", err)
		a.renderTimesheetsError(w, s, "Не удалось удалить запись")
		return
	}
	a.auditOKWorkEntry(r, s.User, "delete", id, existing.EmployeeID, existing.ProjectID, existing.WorkDate, existing.StartTime, existing.DurationMinutes)
	month := normalizeWorksMonth(existing.WorkDate[:7])
	redirect := "/timesheets?works_month=" + month
	if s.Role == RoleAdmin {
		redirect = "/timesheets?employee_id=" + strconv.FormatInt(existing.EmployeeID, 10) + "&works_month=" + month
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *App) renderTimesheetsError(w http.ResponseWriter, s Session, msg string) {
	data := TimesheetsData{
		Username:   s.User,
		Role:       s.Role,
		EmployeeID: s.EmployeeID,
	}
	a.fillTimesheetsMonthData(&data, s.User, normalizeWorksMonth(""))
	data.WorkError = msg
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.Timesheets.Execute(w, data); err != nil {
		log.Printf("ошибка рендера timesheets: %v", err)
	}
}

func (a *App) renderTimesheets(w http.ResponseWriter, data TimesheetsData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpl.Timesheets.Execute(w, data); err != nil {
		log.Printf("ошибка рендера timesheets: %v", err)
	}
}

func (a *App) isEditableWorkDate(role Role, workDate string) bool {
	if role == RoleAdmin {
		return true
	}
	d, err := time.Parse("2006-01-02", workDate)
	if err != nil {
		return false
	}
	limit := time.Now().AddDate(0, 0, -a.cfg.WorkEntriesEditDays)
	dDate := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
	limitDate := time.Date(limit.Year(), limit.Month(), limit.Day(), 0, 0, 0, 0, time.Local)
	return !dDate.Before(limitDate)
}

func (a *App) minWorkDate() string {
	return time.Now().AddDate(0, 0, -a.cfg.WorkEntriesEditDays).Format("2006-01-02")
}

func isWorkDateAllowed(workDate string) bool {
	d, err := time.Parse("2006-01-02", workDate)
	if err != nil {
		return false
	}
	limit := time.Now().AddDate(0, 0, 1)
	dDate := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
	limitDate := time.Date(limit.Year(), limit.Month(), limit.Day(), 0, 0, 0, 0, time.Local)
	return !dDate.After(limitDate)
}

func computeDurationMinutes(startTime, mode, endTime, durHStr, durMStr string) (int64, string) {
	startMin, ok := parseHHMM(startTime)
	if !ok {
		return 0, "Некорректное время начала работ (формат ЧЧ:ММ)"
	}
	var duration int64
	switch mode {
	case "end":
		if endTime == "" {
			return 0, "Укажите время окончания работ"
		}
		endMin, ok := parseHHMM(endTime)
		if !ok {
			return 0, "Некорректное время окончания работ (формат ЧЧ:ММ)"
		}
		if endMin <= startMin {
			return 0, "Время окончания должно быть больше времени начала (перенос на следующие сутки не допускается)"
		}
		duration = int64(endMin - startMin)
	case "duration":
		h, _ := strconv.ParseInt(durHStr, 10, 64)
		m, _ := strconv.ParseInt(durMStr, 10, 64)
		if h < 0 || m < 0 || m > 59 {
			return 0, "Некорректная длительность"
		}
		duration = h*60 + m
	default:
		return 0, "Некорректный режим ввода длительности"
	}
	if duration <= 0 {
		return 0, "Длительность работ должна быть больше нуля"
	}
	if duration > 8*60 {
		return 0, "Длительность работ не может превышать 8 часов (480 минут)"
	}
	if int64(startMin)+duration > 24*60 {
		return 0, "Время начала работ плюс длительность не должно переходить через полночь"
	}
	return duration, ""
}

func parseHHMM(s string) (int, bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func formatFullName(last, first, middle string) string {
	s := last
	if first != "" {
		if s != "" {
			s += " "
		}
		s += first
	}
	if middle != "" {
		if s != "" {
			s += " "
		}
		s += middle
	}
	return s
}

func targetWorkEntry(w *WorkEntry) string {
	return "work_entry(id=" + strconv.FormatInt(w.ID, 10) +
		", employee_id=" + strconv.FormatInt(w.EmployeeID, 10) +
		", project_id=" + strconv.FormatInt(w.ProjectID, 10) +
		", date=" + w.WorkDate +
		", start=" + w.StartTime +
		", duration_min=" + strconv.FormatInt(w.DurationMinutes, 10) + ")"
}

func getPrevAndNextWorkEntries(db *sql.DB, employeeID, excludeID int64, workDate, startTime string) (prev, next *WorkEntry, err error) {
	key := workDate + " " + startTime
	query := `
        SELECT w.id, w.employee_id, e.login, w.project_id, p.name, p.code,
               w.work_date, w.start_time, w.duration_minutes, w.comment
        FROM work_entries w
        JOIN employees e ON e.id = w.employee_id
        JOIN projects  p ON p.id = w.project_id
        WHERE w.employee_id = ? AND w.id <> ?
          AND (w.work_date || ' ' || w.start_time) < ?
        ORDER BY w.work_date DESC, w.start_time DESC, w.id DESC
        LIMIT 1
    `
	var pv WorkEntry
	err = db.QueryRow(query, employeeID, excludeID, key).Scan(
		&pv.ID, &pv.EmployeeID, &pv.EmployeeLogin, &pv.ProjectID,
		&pv.ProjectName, &pv.ProjectCode, &pv.WorkDate, &pv.StartTime,
		&pv.DurationMinutes, &pv.Comment)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	if err == nil {
		prev = &pv
	}
	query = `
        SELECT w.id, w.employee_id, e.login, w.project_id, p.name, p.code,
               w.work_date, w.start_time, w.duration_minutes, w.comment
        FROM work_entries w
        JOIN employees e ON e.id = w.employee_id
        JOIN projects  p ON p.id = w.project_id
        WHERE w.employee_id = ? AND w.id <> ?
          AND (w.work_date || ' ' || w.start_time) > ?
        ORDER BY w.work_date ASC, w.start_time ASC, w.id ASC
        LIMIT 1
    `
	var nx WorkEntry
	err = db.QueryRow(query, employeeID, excludeID, key).Scan(
		&nx.ID, &nx.EmployeeID, &nx.EmployeeLogin, &nx.ProjectID,
		&nx.ProjectName, &nx.ProjectCode, &nx.WorkDate, &nx.StartTime,
		&nx.DurationMinutes, &nx.Comment)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	if err == nil {
		next = &nx
	}
	return prev, next, nil
}