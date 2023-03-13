package script

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
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
