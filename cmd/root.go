package cmd

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/illarion/gonotify"
	"github.com/p2pquake/jmaxml-seis-parser-go/converter"
	"github.com/p2pquake/jmaxml-seis-parser-go/jmaseis"
	"github.com/p2pquake/jmaxml-seis-parser-relay/timestamped"
	"github.com/spf13/cobra"
)

var (
	Version = "develop"
	Commit  = "unknown"
	Date    = "unknown"
)

var rootCmd = &cobra.Command{
	Short:   "XML ファイルの作成を監視し、 EPSP JSON 形式で HTTP リクエストを送信する",
	Version: fmt.Sprintf("%s (commit %s, built at %s)", Version, Commit, Date),
	Run:     run,
}

var cmdWatchDirs []string
var cmdEndpoint string

func Execute() error {
	rootCmd.Flags().StringSliceVarP(&cmdWatchDirs, "directory", "d", []string{"xml"}, "Watching directories")
	rootCmd.Flags().StringVarP(&cmdEndpoint, "endpoint", "e", "http://fluentbit:9880/", "HTTP server endpoint")

	return rootCmd.Execute()
}

func run(cmd *cobra.Command, args []string) {
	log.Printf("Version %s (commit %s, built at %s)", Version, Commit, Date)

	err := checkDirs(cmdWatchDirs)
	if err != nil {
		log.Fatalf("Directory error: %v", err)
	}

	publisher := FluentBitHTTPPublisher{Endpoint: cmdEndpoint}
	watchMovedTo(cmdWatchDirs, publisher.processFile)
}

func watchMovedTo(dirs []string, callback func(string) error) {
	i, err := gonotify.NewInotify()
	if err != nil {
		log.Fatal(err)
	}
	defer i.Close()

	go func() {
		for {
			events, err := i.Read()
			if err != nil {
				log.Fatal(err)
			}

			for _, event := range events {
				name := event.Name
				log.Printf("File detected: %s", name)
				go func() {
					err = callback(name)
					if err != nil {
						// 処理は継続する
						log.Printf("Callback %s error occurred: %v", name, err)
					}
				}()
			}
		}
	}()

	for _, dir := range dirs {
		log.Printf("Watch %s", dir)
		i.AddWatch(dir, gonotify.IN_MOVED_TO)
	}

	<-make(chan struct{})
}

func checkDirs(dirs []string) error {
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			return err
		}
	}
	return nil
}

type FluentBitHTTPPublisher struct {
	Endpoint string
}

func (f FluentBitHTTPPublisher) processFile(filename string) error {
	log.Printf("Process file: %s", filename)

	report, err := readXML(filename)
	if err != nil {
		return err
	}

	jsonData, jmaType, err := convertJmaSeis(filename, report)
	if err != nil {
		return err
	}

	err = publishJSON(f.Endpoint, jsonData, jmaType)
	if err != nil {
		return err
	}

	return nil
}

func readXML(filename string) (*jmaseis.Report, error) {
	log.Print("Read XML...")
	fileBody, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("ReadFile error: %v", err)
		return nil, err
	}

	report := &jmaseis.Report{}
	err = xml.Unmarshal(fileBody, &report)
	if err != nil {
		log.Printf("Unmarshal error: %v", err)
		return nil, err
	}
	return report, nil
}

// VXSE51 震度速報
// VXSE52 地震情報（震源に関する情報）
// VXSE53 地震情報（震源・震度に関する情報）
var earthquakePattern = regexp.MustCompile("VXSE5[123]")

// VTSE41 津波警報・注意報・予報
var tsunamiPattern = regexp.MustCompile("VTSE41")

// VXSE43 緊急地震速報（警報）
// VXSE44 緊急地震速報（警報）・緊急地震速報（予報）
var eewPattern = regexp.MustCompile("VXSE4[34]")

func convertJmaSeis(filename string, report *jmaseis.Report) ([]byte, string, error) {
	log.Print("Convert to JSON...")
	if earthquakePattern.MatchString(filename) {
		jmaType := "earthquake"
		earthquake, err := converter.Vxse2EpspQuake(*report)
		if err != nil {
			return nil, jmaType, err
		}

		errors := converter.ValidateJMAQuake(filename, report, earthquake)
		for _, err = range errors {
			_, ok := err.(converter.ValidationError)
			if ok {
				return nil, jmaType, err
			}
			_, ok = err.(converter.ValidationWarning)
			if ok {
				return nil, jmaType, err
			}
		}

		withTS := timestamped.JMAQuakeWithTimestamp{JMAQuake: *earthquake}
		withTS.Timestamp.Convert = time.Now().Format("2006/01/02 15:04:05.999")

		data, err := json.Marshal(withTS)
		if err != nil {
			return nil, jmaType, err
		}

		return data, jmaType, nil
	}

	if tsunamiPattern.MatchString(filename) {
		jmaType := "tsunami"
		tsunami, err := converter.Vtse2Epsp(*report)
		if err != nil {
			return nil, jmaType, err
		}

		errors := converter.ValidateJMATsunami(filename, report, tsunami)
		for _, err = range errors {
			_, ok := err.(converter.ValidationError)
			if ok {
				return nil, jmaType, err
			}
			_, ok = err.(converter.ValidationWarning)
			if ok {
				return nil, jmaType, err
			}
		}

		withTS := timestamped.JMATsunamiWithTimestamp{JMATsunami: *tsunami}
		withTS.Timestamp.Convert = time.Now().Format("2006/01/02 15:04:05.999")

		data, err := json.Marshal(withTS)
		if err != nil {
			return nil, jmaType, err
		}

		return data, jmaType, nil
	}

	if eewPattern.MatchString(filename) {
		jmaType := "eew"
		eew, err := converter.Vxse2EpspEEW(*report)
		if err != nil {
			return nil, jmaType, err
		}

		withTS := timestamped.JMAEEWWithTimestamp{JMAEEW: *eew}
		withTS.Timestamp.Convert = time.Now().Format("2006/01/02 15:04:05.999")

		data, err := json.Marshal(withTS)
		if err != nil {
			return nil, jmaType, err
		}

		return data, jmaType, err
	}

	return nil, "", fmt.Errorf("no match")
}

func publishJSON(endpoint string, data []byte, jmaType string) error {
	url := fmt.Sprintf("%s%s%s", endpoint, "jma.", jmaType)
	log.Printf("Publish JSON type %s to %s (%d bytes)", jmaType, url, len(data))
	log.Printf("Publish body: %s", data)

	req, err := http.NewRequest(
		"POST",
		url,
		bytes.NewBuffer(data),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-type", "application/json")

	f := func() error {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Publish error occurred: %v", err)
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode > 299 {
			log.Printf("Publish response error: HTTP %d returned", resp.StatusCode)
			return fmt.Errorf("invalid response status code: %d", resp.StatusCode)
		}

		return nil
	}

	err = tryWithBackoff(f)
	if err != nil {
		log.Printf("Publish permanently failed: %v", err)
		return err
	}

	log.Println("Publish succeeded")
	return nil
}

func tryWithBackoff(f func() error) error {
	start := time.Now()
	count := float64(0)

	for {
		err := f()
		if err == nil {
			return nil
		}

		// log.Printf("Try %.0f error: %v", count, err)

		if time.Since(start).Seconds() > 60 {
			log.Println("Time exceeded")
			return err
		}

		count++
		ms := 500 * math.Pow(1.5, count) * (rand.Float64()/2 + 0.75)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}
