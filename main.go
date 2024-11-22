package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/sevlyar/go-daemon"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type Args struct {
	Config string
	Pwd    string
}

type Config struct {
	User       string   `toml:"user"`
	Password   string   `toml:"password"`
	Isp        string   `toml:"isp"`
	Interface  []string `toml:"interface"`
	Errlog     string   `toml:"err_log"`
	Log        string   `toml:"log"`
	ForceStart bool     `toml:"force_start"`
}

type LoginInfo struct {
	Info int `json:"info"`
}

var (
	args   Args
	config Config
)

func loadConfig(filePath string) error {
	_, err := toml.DecodeFile(filePath, &config)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	return nil
}

func login() error {
	loginUrl := fmt.Sprintf(
		"http://10.50.255.11:801/eportal/portal/login?"+
			"callback=dr1003&login_method=1"+
			"&user_account=,0,%s@%s"+
			"&user_password=%s"+
			"&wlan_user_ip=10.38.64.137",
		config.User, config.Isp, config.Password,
	)

	resp, err := http.Get(loginUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var result LoginInfo
	startIdx := findCharIndex(string(body), '(')
	endIdx := findCharIndex(string(body), ')')

	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		return errors.New("invalid response format")
	}

	jsonStr := string(body[startIdx+1 : endIdx])

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return err
	}

	if result.Info != 1 {
		return errors.New("login failed")
	}
	log.Println("Login successful")

	return nil
}

func findCharIndex(s string, ch byte) int {
	for i := range s {
		if s[i] == ch {
			return i
		}
	}
	return -1
}

func networkTest() error {
	resp, err := http.Get("http://www.baidu.com")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func service(wg *sync.WaitGroup, stopChan chan struct{}) {
	defer wg.Done()
	reconnectChan := make(chan struct{})

	go func() {
		for {
			select {
			case <-stopChan:
				return
			default:
				err := networkTest()
				if err != nil {
					log.Println("Network test failed, reconnecting...")
					reconnectChan <- struct{}{}
				} else {
					log.Println("Network test successful")
				}
				time.Sleep(10 * time.Second)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-stopChan:
				return
			case <-reconnectChan:
				if err := login(); err != nil {
					log.Println("Reconnect failed:", err)
				}
			}
		}
	}()
}

func main() {
	configPath := "./config.toml"

	if err := loadConfig(configPath); err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	errLog, err := os.OpenFile(config.Errlog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer errLog.Close()
	log.SetOutput(errLog)

	cntxt := &daemon.Context{
		LogFileName: config.Log,
	}

	d, err := cntxt.Reborn()
	if err != nil {
		log.Fatalf("Unable to run: %s", err)
	}
	if d != nil {
		return
	}
	defer cntxt.Release()

	stopChan := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(1)
	go service(&wg, stopChan)

	if err := login(); err != nil && !config.ForceStart {
		log.Fatalf("Initial login failed, stopping startup: %v", err)
	}

	wg.Wait()
}
