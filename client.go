package main

//  echo -n "test out the server" | nc localhost 3333

import (
	"encoding/json"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang/glog"
	"github.com/pierrre/geohash"
	"github.com/prometheus/client_golang/prometheus"
)

type geoIP struct {
	Ip          string  `json:""`
	CountryCode string  `json:"country_code"`
	CountryName string  `json:"country_name"`
	RegionCode  string  `json:"region_code"`
	RegionName  string  `json:"region_name"`
	City        string  `json:"city"`
	Zipcode     string  `json:"zipcode"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	MetroCode   int     `json:"metro_code"`
	AreaCode    int     `json:"area_code"`
}

func getGeohashAndLocation(address string) (string, string, string, error) {
	var geo geoIP
	response, err := http.Get("https://freegeoip.live/json/" + address)
	if err != nil {
		return "s000", "Unknown", "Unknown", err
	}
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "s000", "Unknown", "Unknown", err
	}

	err = json.Unmarshal(body, &geo)
	if err != nil {
		return "s000", "Unknown", "Unknown", err
	}

	var locations []string
	for _, s := range []string{geo.CountryName, geo.RegionName, geo.City} {
		if strings.TrimSpace(s) != "" {
			locations = append(locations, s)
		}
	}
	location := strings.Join(locations, ", ")
	if location == "" {
		location = "Unknown"
	}
	country := geo.CountryName
	if country == "" {
		country = "Unknown"
	}
	gh := geohash.EncodeAuto(geo.Latitude, geo.Longitude)

	return gh, country, location, nil
}

var letterBytes = []byte(" abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01234567890!@#$%^&*()-=_+[]{}|;:',./<>?")

func randStringBytes(n int64) []byte {
	b := make([]byte, n+1)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	b[n] = '\n'
	return b
}

type client struct {
	conn       net.Conn
	last       time.Time
	next       time.Time
	start      time.Time
	geohash    string
	country    string
	location   string
	bytes_sent int
}

func NewClient(conn net.Conn, interval time.Duration, maxClient int64) *client {
	addr := conn.RemoteAddr().(*net.TCPAddr)
	atomic.AddInt64(&numCurrentClients, 1)
	totalClients.Inc()
	geohash, country, location, err := getGeohashAndLocation(addr.IP.String())
	if err != nil {
		glog.Warningf("Failed to obatin the geohash of %v.", addr.IP)
	}
	clientIP.With(prometheus.Labels{
		"ip":       addr.IP.String(),
		"geohash":  geohash,
		"country":  country,
		"location": location}).Inc()
	glog.V(1).Infof("ACCEPT host=%v port=%v n=%v/%v\n", addr.IP, addr.Port, numCurrentClients, maxClient)
	return &client{
		conn:       conn,
		last:       time.Now(),
		next:       time.Now().Add(interval),
		start:      time.Now(),
		geohash:    geohash,
		country:    country,
		location:   location,
		bytes_sent: 0,
	}
}

func (c *client) Send(bannerMaxLength int64) error {
	length := rand.Int63n(bannerMaxLength)
	bytes_sent, err := c.conn.Write(randStringBytes(length))
	if err != nil {
		return err
	}
	addr := c.conn.RemoteAddr().(*net.TCPAddr)
	secondsSpent := time.Now().Sub(c.last).Seconds()
	c.bytes_sent += bytes_sent
	c.last = time.Now()
	totalBytes.Add(float64(bytes_sent))
	totalSeconds.Add(secondsSpent)
	clientSeconds.With(prometheus.Labels{"ip": addr.IP.String()}).Add(secondsSpent)
	return nil
}

func (c *client) Close() {
	addr := c.conn.RemoteAddr().(*net.TCPAddr)
	atomic.AddInt64(&numCurrentClients, -1)
	totalClientsClosed.Inc()
	glog.V(1).Infof("CLOSE host=%v port=%v time=%v bytes=%v\n", addr.IP, addr.Port, time.Now().Sub(c.start).Seconds(), c.bytes_sent)
	c.conn.Close()
}