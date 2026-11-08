package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
)

func (a *App) exportTimesheetsHandler(w http.ResponseWriter, r *http.Request) {
	s, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
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
	emp, err := getEmployee(a.db, employeeID)
	if err != nil {
		log.Printf("экспорт: ошибка чтения сотрудника %d: %v", employeeID, err)
		http.Error(w, "Employee not found", http.StatusNotFound)
		return
	}
	worksMonth := normalizeWorksMonth(r.URL.Query().Get("works_month"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope != "all" {
		scope = "month"
	}
	var (
		xlsxBytes []byte
		filename  string
	)
	if scope == "all" {
		months, err := listWorkEntryMonths(a.db, employeeID)
		if err != nil {
			log.Printf("экспорт: ошибка чтения списка месяцев: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if len(months) == 0 {
			months = []string{worksMonth}
		}
		all := make([]sheetData, 0, len(months))
		for _, m := range months {
			es, err := listWorkEntriesByMonth(a.db, employeeID, m)
			if err != nil {
				log.Printf("экспорт: ошибка чтения записей за %s: %v", m, err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			all = append(all, sheetData{month: m, entries: es})
		}
		xlsxBytes, err = buildTimesheetXLSXAll(emp, all)
		if err != nil {
			log.Printf("экспорт: ошибка сборки xlsx (все месяцы): %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		filename = "timesheet_" + emp.Login + "_all.xlsx"
	} else {
		entries, err := listWorkEntriesByMonth(a.db, employeeID, worksMonth)
		if err != nil {
			log.Printf("экспорт: ошибка чтения записей табеля: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		xlsxBytes, err = buildTimesheetXLSX(emp, worksMonth, entries)
		if err != nil {
			log.Printf("экспорт: ошибка сборки xlsx: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		filename = "timesheet_" + emp.Login + "_" + worksMonth + ".xlsx"
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(xlsxBytes)))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(xlsxBytes); err != nil {
		log.Printf("экспорт: ошибка записи ответа: %v", err)
	}
	auditOK(s.User, "export_timesheets",
		targetEmployee(emp.ID, emp.Login)+" scope="+scope+" month="+worksMonth)
}

func (a *App) groupStatsExportHandler(w http.ResponseWriter, r *http.Request) {
	s, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if s.Role != RoleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimSpace(r.URL.Query().Get("group_id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid group_id", http.StatusBadRequest)
		return
	}
	group, err := getGroup(a.db, id)
	if err != nil {
		if errors.Is(err, errGroupNotFound) {
			http.Error(w, "Group not found", http.StatusNotFound)
			return
		}
		log.Printf("экспорт статистики отдела: ошибка чтения отдела %d: %v", id, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	worksMonth := normalizeWorksMonth(r.URL.Query().Get("works_month"))
	months, err := listGroupWorkMonths(a.db, id)
	if err != nil {
		log.Printf("экспорт статистики отдела: ошибка чтения списка месяцев: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if len(months) == 0 {
		months = []string{worksMonth}
	}
	var targetMonths []string
	if scope == "all" {
		targetMonths = months
	} else {
		targetMonths = []string{worksMonth}
	}
	sheets := make([]groupSheetData, 0, len(targetMonths))
	for _, m := range targetMonths {
		entries, err := listWorkEntriesByGroup(a.db, id)
		if err != nil {
			log.Printf("экспорт статистики отдела: ошибка чтения записей за %s: %v", m, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		var monthEntries []WorkEntry
		for _, e := range entries {
			if e.WorkDate[:7] == m {
				monthEntries = append(monthEntries, e)
			}
		}
		sheets = append(sheets, groupSheetData{month: m, entries: monthEntries})
	}
	xlsxBytes, err := buildGroupStatsXLSX(group, sheets)
	if err != nil {
		log.Printf("экспорт статистики отдела: ошибка сборки xlsx: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	suffix := "all"
	if scope != "all" {
		suffix = worksMonth
	}
	filename := "group_stats_" + group.Name + "_" + suffix + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(xlsxBytes)))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(xlsxBytes); err != nil {
		log.Printf("экспорт статистики отдела: ошибка записи ответа: %v", err)
	}
	auditOK(s.User, "export_group_stats", targetGroup(group.ID, group.Name)+" scope="+scope)
}

func (a *App) employeeStatsExportHandler(w http.ResponseWriter, r *http.Request) {
	s, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if s.Role != RoleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimSpace(r.URL.Query().Get("group_id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid group_id", http.StatusBadRequest)
		return
	}
	group, err := getGroup(a.db, id)
	if err != nil {
		log.Printf("экспорт статистики сотрудников: ошибка чтения отдела %d: %v", id, err)
		http.Error(w, "Group not found", http.StatusNotFound)
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	worksMonth := normalizeWorksMonth(r.URL.Query().Get("works_month"))
	months, err := listGroupWorkMonths(a.db, id)
	if err != nil {
		log.Printf("экспорт статистики сотрудников: ошибка чтения списка месяцев: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if len(months) == 0 {
		months = []string{worksMonth}
	}
	var targetMonths []string
	if scope == "all" {
		targetMonths = months
	} else {
		targetMonths = []string{worksMonth}
	}
	sheets := make([]employeeSheetData, 0, len(targetMonths))
	for _, m := range targetMonths {
		entries, err := listWorkEntriesByGroupAndMonth(a.db, id, m)
		if err != nil {
			log.Printf("экспорт статистики сотрудников: ошибка чтения записей за %s: %v", m, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		sheets = append(sheets, employeeSheetData{month: m, entries: entries})
	}
	xlsxBytes, err := buildEmployeeStatsXLSX(group.Name, sheets)
	if err != nil {
		log.Printf("экспорт статистики сотрудников: ошибка сборки xlsx: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	suffix := "all"
	if scope != "all" {
		suffix = worksMonth
	}
	cleanGroupName := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, group.Name)
	filename := "employees_stats_" + cleanGroupName + "_" + suffix + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(xlsxBytes)))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(xlsxBytes); err != nil {
		log.Printf("экспорт статистики сотрудников: ошибка записи ответа: %v", err)
	}
	auditOK(s.User, "export_employee_stats", targetGroup(group.ID, group.Name)+" scope="+scope)
}

func buildTimesheetXLSX(emp *Employee, worksMonth string, entries []WorkEntry) ([]byte, error) {
	return buildTimesheetXLSXSheets(emp, []sheetData{
		{month: worksMonth, entries: entries},
	})
}

type sheetData struct {
	month   string
	entries []WorkEntry
}

type groupSheetData struct {
	month   string
	entries []WorkEntry
}

type employeeSheetData struct {
	month   string
	entries []WorkEntry
}

func buildTimesheetXLSXAll(emp *Employee, sheets []sheetData) ([]byte, error) {
	return buildTimesheetXLSXSheets(emp, sheets)
}

func buildTimesheetXLSXSheets(emp *Employee, sheets []sheetData) ([]byte, error) {
	if len(sheets) == 0 {
		sheets = []sheetData{{month: "", entries: nil}}
	}
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	files := make([]struct {
		name string
		body []byte
	}, 0, 4+2*len(sheets))
	files = append(files,
		struct {
			name string
			body []byte
		}{"[Content_Types].xml", []byte(buildContentTypesXML(len(sheets)))},
		struct {
			name string
			body []byte
		}{"_rels/.rels", []byte(rootRelsXML)},
		struct {
			name string
			body []byte
		}{"xl/workbook.xml", []byte(buildWorkbookXMLMulti(sheets))},
		struct {
			name string
			body []byte
		}{"xl/_rels/workbook.xml.rels", []byte(buildWorkbookRelsXMLMulti(len(sheets)))},
		struct {
			name string
			body []byte
		}{"xl/styles.xml", []byte(stylesXML)},
	)
	for i, sh := range sheets {
		files = append(files, struct {
			name string
			body []byte
		}{
			fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),
			[]byte(buildSheetXML(emp, sh.month, sh.entries)),
		})
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(f.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildGroupStatsXLSX(group *Group, sheets []groupSheetData) ([]byte, error) {
	if len(sheets) == 0 {
		sheets = []groupSheetData{{month: "", entries: nil}}
	}
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	files := make([]struct {
		name string
		body []byte
	}, 0, 4+2*len(sheets))
	files = append(files,
		struct {
			name string
			body []byte
		}{"[Content_Types].xml", []byte(buildContentTypesXML(len(sheets)))},
		struct {
			name string
			body []byte
		}{"_rels/.rels", []byte(rootRelsXML)},
		struct {
			name string
			body []byte
		}{"xl/workbook.xml", []byte(buildGroupWorkbookXML(sheets))},
		struct {
			name string
			body []byte
		}{"xl/_rels/workbook.xml.rels", []byte(buildWorkbookRelsXMLMulti(len(sheets)))},
		struct {
			name string
			body []byte
		}{"xl/styles.xml", []byte(stylesXML)},
	)
	for i, sh := range sheets {
		files = append(files, struct {
			name string
			body []byte
		}{
			fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),
			[]byte(buildGroupStatsSheetXML(group, sh.month, sh.entries)),
		})
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(f.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildEmployeeStatsXLSX(groupName string, sheets []employeeSheetData) ([]byte, error) {
	if len(sheets) == 0 {
		sheets = []employeeSheetData{{month: "", entries: nil}}
	}
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	files := make([]struct {
		name string
		body []byte
	}, 0, 4+2*len(sheets))
	files = append(files,
		struct {
			name string
			body []byte
		}{"[Content_Types].xml", []byte(buildContentTypesXML(len(sheets)))},
		struct {
			name string
			body []byte
		}{"_rels/.rels", []byte(rootRelsXML)},
		struct {
			name string
			body []byte
		}{"xl/workbook.xml", []byte(buildEmployeeWorkbookXML(sheets))},
		struct {
			name string
			body []byte
		}{"xl/_rels/workbook.xml.rels", []byte(buildWorkbookRelsXMLMulti(len(sheets)))},
		struct {
			name string
			body []byte
		}{"xl/styles.xml", []byte(stylesXML)},
	)
	for i, sh := range sheets {
		files = append(files, struct {
			name string
			body []byte
		}{
			fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),
			[]byte(buildEmployeeStatsSheetXML(groupName, sh.month, sh.entries)),
		})
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(f.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func colWidthFromMaxLen(maxLen int) float64 {
	if maxLen <= 0 {
		maxLen = 1
	}
	w := float64(maxLen)*1.2 + 2
	return math.Round(w*100) / 100
}

func buildContentTypesXML(sheetCount int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	b.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := 1; i <= sheetCount; i++ {
		fmt.Fprintf(&b, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i)
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func buildWorkbookXMLMulti(sheets []sheetData) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `)
	b.WriteString(`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, sh := range sheets {
		name := monthLabel(sh.month)
		if name == "" {
			name = fmt.Sprintf("Лист%d", i+1)
		}
		r := []rune(name)
		if len(r) > 31 {
			name = string(r[:31])
		}
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`,
			xmlEscape(name), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func buildGroupWorkbookXML(sheets []groupSheetData) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `)
	b.WriteString(`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, sh := range sheets {
		name := monthLabel(sh.month)
		if name == "" {
			name = fmt.Sprintf("Лист%d", i+1)
		}
		r := []rune(name)
		if len(r) > 31 {
			name = string(r[:31])
		}
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`,
			xmlEscape(name), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func buildEmployeeWorkbookXML(sheets []employeeSheetData) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `)
	b.WriteString(`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, sh := range sheets {
		name := monthLabel(sh.month)
		if name == "" {
			name = fmt.Sprintf("Лист%d", i+1)
		}
		r := []rune(name)
		if len(r) > 31 {
			name = string(r[:31])
		}
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`,
			xmlEscape(name), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func buildWorkbookRelsXMLMulti(sheetCount int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= sheetCount; i++ {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i, i)
	}
	fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, sheetCount+1)
	b.WriteString(`</Relationships>`)
	return b.String()
}

func buildSheetXML(emp *Employee, worksMonth string, entries []WorkEntry) string {
	maxB := len([]rune("Дата"))
	maxC := len([]rune("Начало"))
	maxD := len([]rune("Окончание"))
	for _, e := range entries {
		if n := len([]rune(e.WorkDate)); n > maxB {
			maxB = n
		}
		if n := len([]rune(e.StartTime)); n > maxC {
			maxC = n
		}
		end := endTimeStr(e.StartTime, e.DurationMinutes)
		if n := len([]rune(end)); n > maxD {
			maxD = n
		}
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	b.WriteString(`<cols>`)
	b.WriteString(`<col min="1" max="1" width="8.43" customWidth="0" bestFit="1"/>`)
	fmt.Fprintf(&b, `<col min="2" max="2" width="%.2f" customWidth="1"/>`, colWidthFromMaxLen(maxB))
	fmt.Fprintf(&b, `<col min="3" max="3" width="%.2f" customWidth="1"/>`, colWidthFromMaxLen(maxC))
	fmt.Fprintf(&b, `<col min="4" max="4" width="%.2f" customWidth="1"/>`, colWidthFromMaxLen(maxD))
	fmt.Fprintf(&b, `<col min="5" max="5" width="%.2f" customWidth="1"/>`,
		colWidthFromMaxLen(len([]rune("Длительность"))))
	b.WriteString(`<col min="6" max="6" width="28" customWidth="0" bestFit="1"/>`)
	b.WriteString(`<col min="7" max="7" width="40" customWidth="1"/>`)
	b.WriteString(`</cols>`)
	b.WriteString(`<sheetData>`)
	fmt.Fprintf(&b, `<row r="1">`)
	fmt.Fprintf(&b, `<c r="A1" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape("Сотрудник"))
	fmt.Fprintf(&b, `<c r="C1" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape(formatFullName(emp.LastName, emp.FirstName, emp.MiddleName)))
	b.WriteString(`</row>`)
	fmt.Fprintf(&b, `<row r="2">`)
	fmt.Fprintf(&b, `<c r="A2" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape("Отдел"))
	fmt.Fprintf(&b, `<c r="C2" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape(emp.GroupName))
	b.WriteString(`</row>`)
	fmt.Fprintf(&b, `<row r="3">`)
	fmt.Fprintf(&b, `<c r="A3" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape("Месяц"))
	fmt.Fprintf(&b, `<c r="C3" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape(worksMonth+" ("+monthLabel(worksMonth)+")"))
	b.WriteString(`</row>`)
	headerCells := []cell{
		{v: "№"},
		{v: "Дата"},
		{v: "Начало"},
		{v: "Окончание"},
		{v: "Длительность\n(ЧЧ:ММ)", style: 9, hasStyle: true},
		{v: "Объект/проект"},
		{v: "Вид работ"},
	}
	writeRow(&b, 5, headerCells, 3, true)
	type dateGroup struct {
		start int
		end   int
	}
	var groups []dateGroup
	rowNum := 6
	for idx, e := range entries {
		start := e.StartTime
		end := endTimeStr(start, e.DurationMinutes)
		durValue := float64(e.DurationMinutes) / 1440.0
		dateCellValue := e.WorkDate
		if idx > 0 && entries[idx-1].WorkDate == e.WorkDate {
			dateCellValue = ""
		}
		cells := []cell{
			{v: strconv.Itoa(idx + 1), style: 11, hasStyle: true},
			{v: dateCellValue},
			{v: start},
			{v: end},
			{style: 10, hasStyle: true, numeric: &durValue},
			{v: e.ProjectCode, style: 8, hasStyle: true},
			{v: e.Comment, style: 8, hasStyle: true},
		}
		writeRow(&b, rowNum, cells, 2, true)
		if idx == 0 || entries[idx-1].WorkDate != e.WorkDate {
			groups = append(groups, dateGroup{start: rowNum, end: rowNum})
		} else {
			groups[len(groups)-1].end = rowNum
		}
		rowNum++
	}
	totalRow := rowNum
	total := int64(0)
	for _, e := range entries {
		total += e.DurationMinutes
	}
	fmt.Fprintf(&b, `<row r="%d">`, totalRow)
	fmt.Fprintf(&b, `<c r="A%d" t="inlineStr" s="7"><is><t xml:space="preserve">%s</t></is></c>`,
		totalRow, xmlEscape("Общая длительность работ (ЧЧ:ММ)"))
	fmt.Fprintf(&b, `<c r="B%d" s="7"/>`, totalRow)
	fmt.Fprintf(&b, `<c r="C%d" s="7"/>`, totalRow)
	fmt.Fprintf(&b, `<c r="D%d" s="7"/>`, totalRow)
	if len(entries) > 0 {
		firstDataRow := 6
		lastDataRow := totalRow - 1
		cachedValue := float64(total) / 1440.0
		fmt.Fprintf(&b, `<c r="E%d" s="10"><f>SUM(E%d:E%d)</f><v>%s</v></c>`,
			totalRow, firstDataRow, lastDataRow,
			strconv.FormatFloat(cachedValue, 'f', -1, 64))
	} else {
		fmt.Fprintf(&b, `<c r="E%d" s="10"><v>0</v></c>`, totalRow)
	}
	b.WriteString(`</row>`)
	shiftRow := totalRow + 1
	fmt.Fprintf(&b, `<row r="%d">`, shiftRow)
	fmt.Fprintf(&b, `<c r="A%d" t="inlineStr" s="7"><is><t xml:space="preserve">%s</t></is></c>`,
		shiftRow, xmlEscape("Общая длительность работ (раб.дн.)"))
	fmt.Fprintf(&b, `<c r="B%d" s="7"/>`, shiftRow)
	fmt.Fprintf(&b, `<c r="C%d" s="7"/>`, shiftRow)
	fmt.Fprintf(&b, `<c r="D%d" s="7"/>`, shiftRow)
	if len(entries) > 0 {
		cachedShifts := float64(total) / 480.0
		fmt.Fprintf(&b, `<c r="E%d" s="12"><f>E%d*1440/480</f><v>%s</v></c>`,
			shiftRow, totalRow,
			strconv.FormatFloat(cachedShifts, 'f', -1, 64))
	} else {
		fmt.Fprintf(&b, `<c r="E%d" s="12"><v>0</v></c>`, shiftRow)
	}
	b.WriteString(`</row>`)
	b.WriteString(`</sheetData>`)
	dateMergesCount := 0
	for _, g := range groups {
		if g.end > g.start {
			dateMergesCount++
		}
	}
	totalMerges := 6 + 2 + dateMergesCount
	fmt.Fprintf(&b, `<mergeCells count="%d">`, totalMerges)
	b.WriteString(`<mergeCell ref="A1:B1"/>`)
	b.WriteString(`<mergeCell ref="C1:F1"/>`)
	b.WriteString(`<mergeCell ref="A2:B2"/>`)
	b.WriteString(`<mergeCell ref="C2:F2"/>`)
	b.WriteString(`<mergeCell ref="A3:B3"/>`)
	b.WriteString(`<mergeCell ref="C3:F3"/>`)
	for _, g := range groups {
		if g.end > g.start {
			fmt.Fprintf(&b, `<mergeCell ref="B%d:B%d"/>`, g.start, g.end)
		}
	}
	fmt.Fprintf(&b, `<mergeCell ref="A%d:D%d"/>`, totalRow, totalRow)
	fmt.Fprintf(&b, `<mergeCell ref="A%d:D%d"/>`, shiftRow, shiftRow)
	b.WriteString(`</mergeCells>`)
	b.WriteString(`</worksheet>`)
	return b.String()
}

func buildGroupStatsSheetXML(group *Group, worksMonth string, entries []WorkEntry) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	b.WriteString(`<cols>`)
	b.WriteString(`<col min="1" max="1" width="8" customWidth="1"/>`)
	b.WriteString(`<col min="2" max="2" width="60" customWidth="1"/>`)
	b.WriteString(`<col min="3" max="3" width="22" customWidth="1"/>`)
	b.WriteString(`<col min="4" max="4" width="25" customWidth="1"/>`)
	b.WriteString(`<col min="5" max="5" width="40" customWidth="1"/>`)
	b.WriteString(`</cols>`)
	b.WriteString(`<sheetData>`)
	fmt.Fprintf(&b, `<row r="1" ht="20" customHeight="1">`)
	fmt.Fprintf(&b, `<c r="A1" t="inlineStr" s="13"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape("Количество времени затраченного на объекты"))
	b.WriteString(`</row>`)
	fmt.Fprintf(&b, `<row r="2">`)
	fmt.Fprintf(&b, `<c r="A2" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape("Отдел"))
	fmt.Fprintf(&b, `<c r="B2" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape(group.Name))
	b.WriteString(`</row>`)
	fmt.Fprintf(&b, `<row r="3">`)
	fmt.Fprintf(&b, `<c r="A3" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape("Месяц"))
	fmt.Fprintf(&b, `<c r="B3" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape(worksMonth+" ("+monthLabel(worksMonth)+")"))
	b.WriteString(`</row>`)
	headerCells := []cell{
		{v: "№", style: 3, hasStyle: true},
		{v: "Объект/проект\n(Кодовое\nобозначение/шифр)", style: 9, hasStyle: true},
		{v: "Общая длительность\nработ\n(ЧЧ:ММ)", style: 9, hasStyle: true},
		{v: "Общая длительность работ\n(раб.дн.)", style: 9, hasStyle: true},
		{v: "Задействованные\nсотрудники", style: 9, hasStyle: true},
	}
	writeRow(&b, 5, headerCells, 3, true)
	type rowData struct {
		projectName string
		projectCode string
		duration    int64
		employees   []string
		empSeen     map[string]bool
	}
	agg := make(map[string]*rowData)
	var keys []string
	for _, e := range entries {
		key := e.ProjectName + "|" + e.ProjectCode
		if _, ok := agg[key]; !ok {
			agg[key] = &rowData{
				projectName: e.ProjectName,
				projectCode: e.ProjectCode,
				empSeen:     make(map[string]bool),
			}
			keys = append(keys, key)
		}
		agg[key].duration += e.DurationMinutes
		empName := formatFullName(e.EmployeeLastName, e.EmployeeFirstName, e.EmployeeMiddleName)
		if empName == "" {
			empName = e.EmployeeLogin
		}
		if empName != "" && !agg[key].empSeen[empName] {
			agg[key].empSeen[empName] = true
			agg[key].employees = append(agg[key].employees, empName)
		}
	}
	rowNum := 6
	idx := 1
	for _, k := range keys {
		rd := agg[k]
		fmt.Fprintf(&b, `<row r="%d">`, rowNum)
		fmt.Fprintf(&b, `<c r="A%d" t="inlineStr" s="11"><is><t>%d</t></is></c>`, rowNum, idx)
		projText := rd.projectName
		if rd.projectCode != "" {
			projText += " (" + rd.projectCode + ")"
		}
		fmt.Fprintf(&b, `<c r="B%d" t="inlineStr" s="8"><is><t xml:space="preserve">%s</t></is></c>`, rowNum, xmlEscape(projText))
		durVal := float64(rd.duration) / 1440.0
		fmt.Fprintf(&b, `<c r="C%d" s="10"><v>%s</v></c>`, rowNum, strconv.FormatFloat(durVal, 'f', -1, 64))
		shifts := float64(rd.duration) / 480.0
		fmt.Fprintf(&b, `<c r="D%d" s="12"><f>C%d*1440/480</f><v>%s</v></c>`, rowNum, rowNum, strconv.FormatFloat(shifts, 'f', -1, 64))
		empList := strings.Join(rd.employees, ",\n")
		fmt.Fprintf(&b, `<c r="E%d" t="inlineStr" s="14"><is><t xml:space="preserve">%s</t></is></c>`, rowNum, xmlEscape(empList))
		b.WriteString(`</row>`)
		rowNum++
		idx++
	}
	b.WriteString(`</sheetData>`)
	b.WriteString(`<mergeCells count="1">`)
	b.WriteString(`<mergeCell ref="A1:E1"/>`)
	b.WriteString(`</mergeCells>`)
	b.WriteString(`</worksheet>`)
	return b.String()
}

func buildEmployeeStatsSheetXML(groupName, worksMonth string, entries []WorkEntry) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	b.WriteString(`<cols>`)
	b.WriteString(`<col min="1" max="1" width="8" customWidth="1"/>`)
	b.WriteString(`<col min="2" max="2" width="40" customWidth="1"/>`)
	b.WriteString(`<col min="3" max="3" width="22" customWidth="1"/>`)
	b.WriteString(`<col min="4" max="4" width="25" customWidth="1"/>`)
	b.WriteString(`</cols>`)
	b.WriteString(`<sheetData>`)
	fmt.Fprintf(&b, `<row r="1" ht="20" customHeight="1">`)
	fmt.Fprintf(&b, `<c r="A1" t="inlineStr" s="13"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape("Количество времени затраченного сотрудниками"))
	b.WriteString(`</row>`)
	fmt.Fprintf(&b, `<row r="2">`)
	fmt.Fprintf(&b, `<c r="A2" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape("Отдел"))
	fmt.Fprintf(&b, `<c r="B2" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape(groupName))
	fmt.Fprintf(&b, `<c r="C2" s="0"/>`)
	b.WriteString(`</row>`)
	fmt.Fprintf(&b, `<row r="3">`)
	fmt.Fprintf(&b, `<c r="A3" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape("Месяц"))
	fmt.Fprintf(&b, `<c r="B3" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape(worksMonth+" ("+monthLabel(worksMonth)+")"))
	fmt.Fprintf(&b, `<c r="C3" s="0"/>`)
	b.WriteString(`</row>`)
	headerCells := []cell{
		{v: "№", style: 3, hasStyle: true},
		{v: "Сотрудник", style: 9, hasStyle: true},
		{v: "Общая длительность\nработ\n(ЧЧ:ММ)", style: 9, hasStyle: true},
		{v: "Общая длительность работ\n(раб.дн.)", style: 9, hasStyle: true},
	}
	writeRow(&b, 5, headerCells, 3, true)
	type rowData struct {
		empName  string
		duration int64
	}
	agg := make(map[string]*rowData)
	var keys []string
	for _, e := range entries {
		empName := formatFullName(e.EmployeeLastName, e.EmployeeFirstName, e.EmployeeMiddleName)
		if empName == "" {
			empName = e.EmployeeLogin
		}
		if _, ok := agg[empName]; !ok {
			agg[empName] = &rowData{empName: empName}
			keys = append(keys, empName)
		}
		agg[empName].duration += e.DurationMinutes
	}
	rowNum := 6
	idx := 1
	for _, k := range keys {
		rd := agg[k]
		fmt.Fprintf(&b, `<row r="%d">`, rowNum)
		fmt.Fprintf(&b, `<c r="A%d" t="inlineStr" s="11"><is><t>%d</t></is></c>`, rowNum, idx)
		fmt.Fprintf(&b, `<c r="B%d" t="inlineStr" s="8"><is><t xml:space="preserve">%s</t></is></c>`, rowNum, xmlEscape(rd.empName))
		durVal := float64(rd.duration) / 1440.0
		fmt.Fprintf(&b, `<c r="C%d" s="10"><v>%s</v></c>`, rowNum, strconv.FormatFloat(durVal, 'f', -1, 64))
		shifts := float64(rd.duration) / 480.0
		fmt.Fprintf(&b, `<c r="D%d" s="12"><f>C%d*1440/480</f><v>%s</v></c>`, rowNum, rowNum, strconv.FormatFloat(shifts, 'f', -1, 64))
		b.WriteString(`</row>`)
		rowNum++
		idx++
	}
	b.WriteString(`</sheetData>`)
	b.WriteString(`<mergeCells count="3">`)
	b.WriteString(`<mergeCell ref="A1:D1"/>`)
	b.WriteString(`<mergeCell ref="B2:C2"/>`)
	b.WriteString(`<mergeCell ref="B3:C3"/>`)
	b.WriteString(`</mergeCells>`)
	b.WriteString(`</worksheet>`)
	return b.String()
}

type cell struct {
	v        string
	style    int
	hasStyle bool
	numeric  *float64
}

func writeRow(b *strings.Builder, rowNum int, cells []cell, style int, writeEmpty bool) {
	fmt.Fprintf(b, `<row r="%d">`, rowNum)
	for i, c := range cells {
		col := columnLetter(i + 1)
		ref := fmt.Sprintf("%s%d", col, rowNum)
		st := style
		if c.hasStyle {
			st = c.style
		}
		if c.numeric != nil {
			fmt.Fprintf(b, `<c r="%s" s="%d"><v>%s</v></c>`,
				ref, st, strconv.FormatFloat(*c.numeric, 'f', -1, 64))
			continue
		}
		if c.v == "" {
			if !writeEmpty {
				continue
			}
			fmt.Fprintf(b, `<c r="%s" s="%d"/>`, ref, st)
			continue
		}
		fmt.Fprintf(b, `<c r="%s" t="inlineStr" s="%d"><is><t xml:space="preserve">%s</t></is></c>`,
			ref, st, xmlEscape(c.v))
	}
	b.WriteString(`</row>`)
}

func columnLetter(n int) string {
	var out []byte
	for n > 0 {
		n--
		out = append([]byte{byte('A' + n%26)}, out...)
		n /= 26
	}
	return string(out)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

func endTimeStr(start string, durationMinutes int64) string {
	h, m, ok := parseHHMMToParts(start)
	if !ok {
		return ""
	}
	total := h*60 + m + int(durationMinutes)
	total %= 24 * 60
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func durationHHMMStr(minutes int64) string {
	if minutes < 0 {
		minutes = 0
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

func buildWorkbookXML(worksMonth string) string {
	sheetName := monthLabel(worksMonth)
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
          xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="` + xmlEscape(sheetName) + `" sheetId="1" r:id="rId1"/>
  </sheets>
</workbook>`
}

func (a *App) individualEmployeeStatsExportHandler(w http.ResponseWriter, r *http.Request) {
	s, ok := currentSession(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if s.Role != RoleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimSpace(r.URL.Query().Get("employee_id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Invalid employee_id", http.StatusBadRequest)
		return
	}
	emp, err := getEmployee(a.db, id)
	if err != nil {
		log.Printf("экспорт статистики сотрудника: ошибка чтения сотрудника %d: %v", id, err)
		http.Error(w, "Employee not found", http.StatusNotFound)
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	worksMonth := normalizeWorksMonth(r.URL.Query().Get("works_month"))
	months, err := listEmployeeWorkMonths(a.db, id)
	if err != nil {
		log.Printf("экспорт статистики сотрудника: ошибка чтения списка месяцев: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if len(months) == 0 {
		months = []string{worksMonth}
	}
	var targetMonths []string
	if scope == "all" {
		targetMonths = months
	} else {
		targetMonths = []string{worksMonth}
	}
	sheets := make([]employeeSheetData, 0, len(targetMonths))
	for _, m := range targetMonths {
		entries, err := listWorkEntriesByEmployeeAndMonth(a.db, id, m)
		if err != nil {
			log.Printf("экспорт статистики сотрудника: ошибка чтения записей за %s: %v", m, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		sheets = append(sheets, employeeSheetData{month: m, entries: entries})
	}
	fullName := formatFullName(emp.LastName, emp.FirstName, emp.MiddleName)
	xlsxBytes, err := buildIndividualEmployeeStatsXLSX(emp.GroupName, fullName, sheets)
	if err != nil {
		log.Printf("экспорт статистики сотрудника: ошибка сборки xlsx: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	suffix := "all"
	if scope != "all" {
		suffix = worksMonth
	}
	cleanName := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, fullName)
	filename := "employee_stats_" + cleanName + "_" + suffix + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(xlsxBytes)))
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write(xlsxBytes); err != nil {
		log.Printf("экспорт статистики сотрудника: ошибка записи ответа: %v", err)
	}
	auditOK(s.User, "export_individual_employee_stats", targetEmployee(emp.ID, emp.Login)+" scope="+scope)
}

func buildIndividualEmployeeStatsXLSX(groupName, empName string, sheets []employeeSheetData) ([]byte, error) {
	if len(sheets) == 0 {
		sheets = []employeeSheetData{{month: "", entries: nil}}
	}
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	files := make([]struct {
		name string
		body []byte
	}, 0, 4+2*len(sheets))
	files = append(files,
		struct{ name string; body []byte }{"[Content_Types].xml", []byte(buildContentTypesXML(len(sheets)))},
		struct{ name string; body []byte }{"_rels/.rels", []byte(rootRelsXML)},
		struct{ name string; body []byte }{"xl/workbook.xml", []byte(buildEmployeeWorkbookXML(sheets))},
		struct{ name string; body []byte }{"xl/_rels/workbook.xml.rels", []byte(buildWorkbookRelsXMLMulti(len(sheets)))},
		struct{ name string; body []byte }{"xl/styles.xml", []byte(stylesXML)},
	)
	for i, sh := range sheets {
		files = append(files, struct{ name string; body []byte }{
			fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),
			[]byte(buildIndividualEmployeeStatsSheetXML(groupName, empName, sh.month, sh.entries)),
		})
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(f.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildIndividualEmployeeStatsSheetXML(groupName, empName, worksMonth string, entries []WorkEntry) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	
	b.WriteString(`<cols>`)
	b.WriteString(`<col min="1" max="1" width="15" customWidth="1"/>`)
	b.WriteString(`<col min="2" max="2" width="60" customWidth="1"/>`)
	b.WriteString(`<col min="3" max="3" width="22" customWidth="1"/>`)
	b.WriteString(`<col min="4" max="4" width="25" customWidth="1"/>`)
	b.WriteString(`</cols>`)
	
	b.WriteString(`<sheetData>`)
	
	fmt.Fprintf(&b, `<row r="1">`)
	fmt.Fprintf(&b, `<c r="A1" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape("Сотрудник:"))
	fmt.Fprintf(&b, `<c r="B1" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape(empName))
	fmt.Fprintf(&b, `<c r="C1" s="0"/>`)
	b.WriteString(`</row>`)
	
	fmt.Fprintf(&b, `<row r="2">`)
	fmt.Fprintf(&b, `<c r="A2" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape("Отдел:"))
	fmt.Fprintf(&b, `<c r="B2" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape(groupName))
	fmt.Fprintf(&b, `<c r="C2" s="0"/>`)
	b.WriteString(`</row>`)
	
	fmt.Fprintf(&b, `<row r="3">`)
	fmt.Fprintf(&b, `<c r="A3" t="inlineStr" s="6"><is><t xml:space="preserve">%s</t></is></c>`, xmlEscape("Месяц:"))
	fmt.Fprintf(&b, `<c r="B3" t="inlineStr" s="0"><is><t xml:space="preserve">%s</t></is></c>`,
		xmlEscape(worksMonth+" ("+monthLabel(worksMonth)+")"))
	fmt.Fprintf(&b, `<c r="C3" s="0"/>`)
	b.WriteString(`</row>`)
	
	headerCells := []cell{
		{v: "№", style: 3, hasStyle: true},
		{v: "Объект/проект\n(Кодовое\nобозначение/шифр)", style: 9, hasStyle: true},
		{v: "Общая длительность\nработ\n(ЧЧ:ММ)", style: 9, hasStyle: true},
		{v: "Общая длительность работ\n(раб.дн.)", style: 9, hasStyle: true},
	}
	writeRow(&b, 5, headerCells, 3, true)
	
	type rowData struct {
		projectName string
		projectCode string
		duration    int64
	}
	agg := make(map[string]*rowData)
	var keys []string
	for _, e := range entries {
		key := e.ProjectName + "|" + e.ProjectCode
		if _, ok := agg[key]; !ok {
			agg[key] = &rowData{
				projectName: e.ProjectName,
				projectCode: e.ProjectCode,
			}
			keys = append(keys, key)
		}
		agg[key].duration += e.DurationMinutes
	}
	
	rowNum := 6
	idx := 1
	for _, k := range keys {
		rd := agg[k]
		fmt.Fprintf(&b, `<row r="%d">`, rowNum)
		fmt.Fprintf(&b, `<c r="A%d" t="inlineStr" s="11"><is><t>%d</t></is></c>`, rowNum, idx)
		projText := rd.projectName
		if rd.projectCode != "" {
			projText += " (" + rd.projectCode + ")"
		}
		fmt.Fprintf(&b, `<c r="B%d" t="inlineStr" s="8"><is><t xml:space="preserve">%s</t></is></c>`, rowNum, xmlEscape(projText))
		durVal := float64(rd.duration) / 1440.0
		fmt.Fprintf(&b, `<c r="C%d" s="10"><v>%s</v></c>`, rowNum, strconv.FormatFloat(durVal, 'f', -1, 64))
		shifts := float64(rd.duration) / 480.0
		fmt.Fprintf(&b, `<c r="D%d" s="12"><f>C%d*1440/480</f><v>%s</v></c>`, rowNum, rowNum, strconv.FormatFloat(shifts, 'f', -1, 64))
		b.WriteString(`</row>`)
		rowNum++
		idx++
	}
	
	b.WriteString(`</sheetData>`)
	
	b.WriteString(`<mergeCells count="3">`)
	b.WriteString(`<mergeCell ref="B1:C1"/>`)
	b.WriteString(`<mergeCell ref="B2:C2"/>`)
	b.WriteString(`<mergeCell ref="B3:C3"/>`)
	b.WriteString(`</mergeCells>`)
	
	b.WriteString(`</worksheet>`)
	return b.String()
}

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <numFmts count="2">
    <numFmt numFmtId="165" formatCode="[hh]:mm"/>
    <numFmt numFmtId="166" formatCode="0.00"/>
  </numFmts>
  <fonts count="2">
    <font><sz val="11"/><name val="Calibri"/></font>
    <font><b/><sz val="11"/><name val="Calibri"/></font>
  </fonts>
  <fills count="1">
    <fill><patternFill patternType="none"/></fill>
  </fills>
  <borders count="2">
    <border><left/><right/><top/><bottom/><diagonal/></border>
    <border>
      <left style="thin"><color rgb="FF000000"/></left>
      <right style="thin"><color rgb="FF000000"/></right>
      <top style="thin"><color rgb="FF000000"/></top>
      <bottom style="thin"><color rgb="FF000000"/></bottom>
      <diagonal/>
    </border>
  </borders>
  <cellStyleXfs count="2">
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0"/>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="1" applyBorder="1"/>
  </cellStyleXfs>
  <cellXfs count="15">
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="center"/></xf>
    <xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>
    <xf numFmtId="0" fontId="1" fillId="0" borderId="1" xfId="1" applyFont="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="1" applyBorder="1" applyAlignment="1"><alignment horizontal="right" vertical="center"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="1" applyBorder="1" applyAlignment="1"><alignment horizontal="left" vertical="center"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment horizontal="left" vertical="center"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="1" applyBorder="1" applyAlignment="1"><alignment vertical="center" wrapText="1"/></xf>
    <xf numFmtId="0" fontId="1" fillId="0" borderId="1" xfId="1" applyFont="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf>
    <xf numFmtId="165" fontId="0" fillId="0" borderId="1" xfId="1" applyNumberFormat="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="1" applyBorder="1" applyAlignment="1"><alignment horizontal="left" vertical="center"/></xf>
    <xf numFmtId="166" fontId="0" fillId="0" borderId="1" xfId="1" applyNumberFormat="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>
    <xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment horizontal="left" vertical="center"/></xf>
    <xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="1" applyBorder="1" applyAlignment="1"><alignment horizontal="left" vertical="center" wrapText="1"/></xf>
  </cellXfs>
</styleSheet>`