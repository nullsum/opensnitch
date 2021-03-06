package ui

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/gustavo-iniguez-goya/opensnitch/daemon/log"
	"github.com/gustavo-iniguez-goya/opensnitch/daemon/procmon"
	"github.com/gustavo-iniguez-goya/opensnitch/daemon/rule"
)

func (c *Client) loadDiskConfiguration(reload bool) {
	raw, err := ioutil.ReadFile(configFile)
	if err != nil {
		fmt.Errorf("Error loading disk configuration %s: %s", configFile, err)
	}

	if ok := c.loadConfiguration(raw); ok {
		if err := c.configWatcher.Add(configFile); err != nil {
			log.Error("Could not watch path: %s", err)
			return
		}
	}

	if reload {
		return
	}

	go c.monitorConfigWorker()
}

func (c *Client) loadConfiguration(rawConfig []byte) bool {
	config.Lock()
	defer config.Unlock()

	if err := json.Unmarshal(rawConfig, &config); err != nil {
		fmt.Errorf("Error parsing configuration %s: %s", configFile, err)
		return false
	}

	if config.DefaultAction != "" {
		clientDisconnectedRule.Action = rule.Action(config.DefaultAction)
		clientErrorRule.Action = rule.Action(config.DefaultAction)
	}
	if config.DefaultDuration != "" {
		clientDisconnectedRule.Duration = rule.Duration(config.DefaultDuration)
		clientErrorRule.Duration = rule.Duration(config.DefaultDuration)
	}
	if config.LogLevel != nil {
		log.MinLevel = int(*config.LogLevel)
	}
	if config.ProcMonitorMethod != "" {
		procmon.MonitorMethod = config.ProcMonitorMethod
	}

	return true
}

func (c *Client) saveConfiguration(rawConfig string) error {
	conf, err := json.Marshal([]byte(rawConfig))
	if err != nil {
		log.Error("saving json configuration: ", err, conf)
		return err
	}

	if c.loadConfiguration([]byte(rawConfig)) != true {
		return fmt.Errorf("Error parsing configuration %s: %s", rawConfig, err)
	}

	if err = ioutil.WriteFile(configFile, []byte(rawConfig), 0644); err != nil {
		log.Error("writing configuration to disk: ", err)
		return err
	}
	return nil
}
