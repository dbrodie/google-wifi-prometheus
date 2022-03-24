package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	InfoLogger    *log.Logger
	WarningLogger *log.Logger
	ErrorLogger   *log.Logger
)

type GoogleWifi struct {
	apiToken string
	groupId  string
}

func (gw *GoogleWifi) initApiKey() error {
	req, err := http.NewRequest("POST", "https://oauthaccountmanager.googleapis.com/v1/issuetoken?app_id=com.google.OnHub&client_id=586698244315-vc96jg3mn4nap78iir799fc2ll3rk18s.apps.googleusercontent.com&hl=en-US&lib_ver=3.3&response_type=token&scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Faccesspoints+https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fclouddevices", nil)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}

	InfoLogger.Println("Refresh Token ", os.Getenv("GW_REFRESH_TOKEN"))

	req.Header.Set("Authorization", "Bearer "+os.Getenv("GW_REFRESH_TOKEN"))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}
	defer resp.Body.Close()

	respData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}

	respMap := make(map[string]interface{})
	err = json.Unmarshal(respData, &respMap)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}

	token, ok := respMap["token"].(string)
	if !ok {
		ErrorLogger.Println(resp.Body)
		return err
	}

	gw.apiToken = token

	return nil
}

func (gw *GoogleWifi) initGroups() error {

	req, err := http.NewRequest("GET", "https://accesspoints.googleapis.com/v2/groups", nil)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+gw.apiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}
	defer resp.Body.Close()

	InfoLogger.Println("initGroups Query status ", resp.Status)

	respData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}

	respMap := make(map[string]interface{})
	err = json.Unmarshal(respData, &respMap)
	if err != nil {
		ErrorLogger.Println(err)
		return err
	}

	groups := respMap["groups"].([]interface{})
	group0 := groups[0].(map[string]interface{})

	gw.groupId = group0["id"].(string)

	return nil
}

func createGoogleWifi() (*GoogleWifi, error) {

	googleWifi := &GoogleWifi{}

	err := googleWifi.initApiKey()
	if err != nil {
		ErrorLogger.Println(err)
		return nil, err
	}

	// InfoLogger.Println("token ", googleWifi.apiToken)

	err = googleWifi.initGroups()
	if err != nil {
		ErrorLogger.Println(err)
		return nil, err
	}

	InfoLogger.Println("groupId ", googleWifi.groupId)

	return googleWifi, nil

}

type GoogleWifiMetrics struct {
	deviceId         string
	transmitSpeedBps int
	receiveSpeedBps  int
}

func (gw *GoogleWifi) getBandwidthMetrics() ([]GoogleWifiMetrics, error) {
	req, err := http.NewRequest("GET", "https://accesspoints.googleapis.com/v2/groups/"+gw.groupId+"/realtimeMetrics", nil)
	if err != nil {
		ErrorLogger.Println(err)
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+gw.apiToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ErrorLogger.Println(err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		ErrorLogger.Println("getBandwidthMetrics Query status ", resp.Status)
		return nil, errors.New("getBandwidthMetrics HTTP Request failed")
	}

	respData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		ErrorLogger.Println(err)
		return nil, err
	}

	respMap := make(map[string]interface{})
	err = json.Unmarshal(respData, &respMap)
	if err != nil {
		ErrorLogger.Println(err)
		return nil, err
	}

	stationMetrics, ok := respMap["stationMetrics"].([]interface{})
	if !ok {
		ErrorLogger.Println(("No station metrics??"))
		return nil, errors.New("Null station metrics")
	}

	var res []GoogleWifiMetrics

	for _, entry := range stationMetrics {
		metric := GoogleWifiMetrics{}
		stationMetric := entry.(map[string]interface{})

		station := stationMetric["station"].(map[string]interface{})
		metric.deviceId = station["friendlyName"].(string)

		traffic, ok := stationMetric["traffic"].(map[string]interface{})

		if !ok {
			WarningLogger.Println("Entry empty??", metric.deviceId)
			continue
		}

		var vals string
		var vali int
		vals, ok = traffic["transmitSpeedBps"].(string)
		if ok {
			vali, err = strconv.Atoi(vals)
			if err != nil {
				ErrorLogger.Println(err)
				return nil, err
			}
			metric.transmitSpeedBps = vali
		}

		vals, ok = traffic["receiveSpeedBps"].(string)
		if ok {
			vali, err = strconv.Atoi(vals)
			if err != nil {
				ErrorLogger.Println(err)
				return nil, err
			}
			metric.receiveSpeedBps = vali
		}

		res = append(res, metric)
	}

	return res, nil
}

func recordBandwidthMetrcis(googleWifi *GoogleWifi) {
	go func() {
		for {
			metrics, err := googleWifi.getBandwidthMetrics()
			if err != nil {
				ErrorLogger.Fatal(err)
			}

			for _, metric := range metrics {
				bandwidth_upload.WithLabelValues(metric.deviceId).Set(float64(metric.transmitSpeedBps))
				bandwidth_download.WithLabelValues(metric.deviceId).Set(float64(metric.receiveSpeedBps))
			}

			time.Sleep(5 * time.Second)
		}
	}()
}

var (
	bandwidth_upload = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "google_wifi",
		Subsystem: "bandwidth",
		Name:      "upload",
	},
		[]string{
			"deviceId",
		})
	bandwidth_download = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "google_wifi",
		Subsystem: "bandwidth",
		Name:      "download",
	},
		[]string{
			"deviceId",
		})
)

func main() {
	InfoLogger = log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	WarningLogger = log.New(os.Stderr, "WARNING: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)

	googleWifi, err := createGoogleWifi()

	if err != nil {
		ErrorLogger.Fatal(err)
	}

	recordBandwidthMetrcis(googleWifi)

	InfoLogger.Println("Starting!")

	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(":2112", nil))
}
