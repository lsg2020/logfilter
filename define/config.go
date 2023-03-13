package define

type ConfigTarget struct {
	ID      string               `json:"id"`
	Open    bool                 `json:"open"`
	Filters []string             `json:"filters"`
	Files   []*ConfigLogFileInfo `json:"files"`
}

type ConfigLogFileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	SshHost string `json:"ssh_host"`
	SshPort int    `json:"ssh_port"`
	SshUser string `json:"ssh_user"`
	SshPwd  string `json:"ssh_pwd"`
	SshKey  string `json:"ssh_key"`
}

type ConfigFilterInfo struct {
	ID                string `json:"id"`
	Desc              string `json:"desc"`
	Script            string `json:"script"`
	CheckFunctionName string `json:"check_function_name"`
	SubFilters        []struct {
		ID                  string `json:"id"`
		Desc                string `json:"desc"`
		Amount              int    `json:"amount"`
		CheckFunctionName   string `json:"check_function_name"`
		SummaryFunctionName string `json:"summary_function_name"`
	} `json:"sub_filters"`
}

type Config struct {
	Address       string              `json:"address"`
	Port          int                 `json:"port"`
	ReloadSeconds int                 `json:"reload_seconds"`
	AdminUser     string              `json:"admin_user"`
	AdminPwd      string              `json:"admin_pwd"`
	Targets       []*ConfigTarget     `json:"targets"`
	Filters       []*ConfigFilterInfo `json:"filters"`
}

func (c *Config) GetTarget(id string) *ConfigTarget {
	for _, t := range c.Targets {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (c *Config) GetTargetFile(id string, file string) *ConfigLogFileInfo {
	target := c.GetTarget(id)
	if target == nil {
		return nil
	}
	for _, f := range target.Files {
		if f.Name == file {
			return f
		}
	}
	return nil
}

func (c *Config) GetFilter(id string) *ConfigFilterInfo {
	for _, filter := range c.Filters {
		if filter.ID == id {
			return filter
		}
	}
	return nil
}
