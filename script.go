package script

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"logfilter"
)

//=============================== event ================================================
// ERROR test event {"req_type": "event1"}
var vModuleEventType = regexp.MustCompile(`"req_type": "([^"]*)"`)
var vModuleEventTotal = make(map[string]int)

func ModuleEvent(str string) (ok bool, summary string, ignore bool, err error) {
	if strings.Contains(str, "test event") {
		RecordAmount++
		ok = true
		summary = findString(vModuleEventType, str)
		vModuleEventTotal[summary]++
		return
	}
	return
}

func ModuleEventSummary() string {
	return debugNameAmount(vModuleEventTotal)
}

//=============================== base ================================================

var RecordAmount = 0
var RecordModule string

func CheckBase(str string) (ok bool, summary string, ignore bool, err error) {
	if !strings.Contains(str, "ERROR") {
		return
	}

	ok = true
	RecordAmount = 0
	RecordModule = findString(vCheckUnknownCallModule, str)
	return
}

var vCheckUnknownServiceTotal = make(map[string]int)
var vCheckUnknownCallModule = regexp.MustCompile(`"module": "([^"]*)"`)

func CheckUnknown(str string) (ok bool, summary string, ignore bool, err error) {
	if RecordAmount == 0 {
		summary = RecordModule
		vCheckUnknownServiceTotal[summary]++
		ok = true
		return
	}
	return
}

func CheckUnknownSummary() string {
	return debugNameAmount(vCheckUnknownServiceTotal)
}

//=============================== help ================================================

func findString(r *regexp.Regexp, str string) string {
	result := r.FindStringSubmatch(str)
	if len(result) != 2 {
		return ""
	}
	return result[1]
}

type NameAmountInfo struct {
	Name   string
	Amount int
}

func debugNameAmount(data map[string]int) string {
	list := make([]NameAmountInfo, 0)
	for k, v := range data {
		list = append(list, NameAmountInfo{Name: k, Amount: v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Amount > list[j].Amount })
	buff, err := json.Marshal(list)
	if err != nil {
		return err.Error()
	}
	if len(buff) > 1024 {
		return string(buff[:1024]) + "..."
	}
	return string(buff)
}

type FilterInfo struct {
	Name    string
	Amount  int
	Func    func(str string) (ok bool, summary string, ignore bool, err error)
	Summary func() string

	Files    []string
	Lines    []string
	Summarys []string

	TotalAmount  int
	IgnoreAmount int
	PrintAmount  int
}

func (f *FilterInfo) log(file string, str string) {
	ok, summary, ignore, _ := f.Func(str)
	if !ok {
		return
	}

	f.TotalAmount++
	if ignore {
		f.IgnoreAmount++
		return
	}
	f.PrintAmount++

	f.Files = append(f.Files, file)
	f.Lines = append(f.Lines, str)
	f.Summarys = append(f.Summarys, summary)

	l := len(f.Files)
	if l > f.Amount {
		f.Files = f.Files[l-f.Amount:]
		f.Lines = f.Lines[l-f.Amount:]
		f.Summarys = f.Summarys[l-f.Amount:]
	}
}

type FilterList []*FilterInfo

func (fs FilterList) getFilter(name string) *FilterInfo {
	for _, f := range fs {
		if f.Name == name {
			return f
		}
	}
	return nil
}

var filters = FilterList{
	{
		Name:    "event",
		Amount:  100,
		Func:    ModuleEvent,
		Summary: ModuleEventSummary,
	},
	{
		Name:    "unknown",
		Amount:  100,
		Func:    CheckUnknown,
		Summary: CheckUnknownSummary,
	},
}

func Entry(param *logfilter.ScriptParam) {
	if param.Type == "log" {
		ok, _, _, _ := CheckBase(param.ReqLogStr)
		if !ok {
			return
		}
		for _, f := range filters {
			f.log(param.ReqLogFile, param.ReqLogStr)
		}
	}
	if param.Type == "filters" {
		for _, f := range filters {
			param.ResFilters = append(param.ResFilters, f.Name)
		}
	}
	if param.Type == "records" {
		f := filters.getFilter(param.ReqRecordsFilter)
		if f == nil {
			return
		}
		param.ResRecordsSummary = append(param.ResRecordsSummary, fmt.Sprintf("total:%d ignore:%d print:%d", f.TotalAmount, f.IgnoreAmount, f.PrintAmount))
		if f.Summary != nil {
			param.ResRecordsLogs = append(param.ResRecordsLogs, f.Summary())
		} else {
			param.ResRecordsLogs = append(param.ResRecordsLogs, "")
		}
		for i := len(f.Summarys) - 1; i >= 0; i-- {
			param.ResRecordsSummary = append(param.ResRecordsSummary, f.Summarys[i])
			param.ResRecordsLogs = append(param.ResRecordsLogs, f.Lines[i])
		}
	}
}
