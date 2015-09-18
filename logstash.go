package logstash

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"strings"

	"github.com/gliderlabs/logspout/router"
)

func init() {
	router.AdapterFactories.Register(NewLogstashAdapter, "logstash")
}

// LogstashAdapter is an adapter that streams JSON to Logstash via TCP.
type LogstashAdapter struct {
	conn  net.Conn
	route *router.Route
}

func getopt(name, dfault string) string {
	value := os.Getenv(name)
	if value == "" {
		value = dfault
	}
	return value
}

// NewLogstashAdapter creates a LogstashAdapter with TCP as the default transport.
func NewLogstashAdapter(route *router.Route) (router.LogAdapter, error) {
	transport, found := router.AdapterTransports.Lookup(route.AdapterTransport("tcp"))
	if !found {
		return nil, errors.New("unable to find adapter: " + route.Adapter)
	}

	conn, err := transport.Dial(route.Address, route.Options)
	if err != nil {
		return nil, err
	}

	return &LogstashAdapter{
		route: route,
		conn:  conn,
	}, nil
}

func GetLogspoutOptionsString(env []string) string {
	if env != nil {
		for _, value := range env {
			if strings.HasPrefix(value, "LOGSPOUT_OPTIONS=") {
				return strings.TrimPrefix(value, "LOGSPOUT_OPTIONS=")
			}
		}
	}
	return ""
}

func UnmarshalOptions(opt_string string) map[string]string {
	var options map[string]string

	if opt_string != "" {
		b := []byte(opt_string)

		json.Unmarshal(b, &options)
		return options
	}
	return nil
}

// Stream implements the router.LogAdapter interface.
func (a *LogstashAdapter) Stream(logstream chan *router.Message) {

	options := UnmarshalOptions(getopt("OPTIONS", ""))

	for m := range logstream {
		var msg interface{}

		container_options := UnmarshalOptions(GetLogspoutOptionsString(m.Container.Config.Env))

		// We give preference to the containers environment that is sending us the message
		if container_options == nil {
			container_options = options
		} else if options != nil {
			for k, v := range options {
				if _, ok := container_options[k]; !ok {
					container_options[k] = v
				}
			}
		}

		var jsonMsg map[string]interface{}
		err := json.Unmarshal([]byte(m.Data), &jsonMsg)
		if err != nil {
			// the message is not in JSON make a new JSON message
			msg = LogstashMessage{
				Message:  m.Data,
				Name:     m.Container.Name,
				ID:       m.Container.ID,
				Image:    m.Container.Config.Image,
				Hostname: m.Container.Config.Hostname,
				Args:     m.Container.Args,
				Options:  container_options,
			}

		} else {
			// the message is already in JSON just add the docker specific fields
			jsonMsg["docker.name"] = m.Container.Name
			jsonMsg["docker.id"] = m.Container.ID
			jsonMsg["docker.image"] = m.Container.Config.Image
			jsonMsg["docker.hostname"] = m.Container.Config.Hostname
			jsonMsg["docker.args"] = m.Container.Args
			jsonMsg["options"] = container_options
			msg = jsonMsg
		}

		js, err := json.Marshal(msg)
		if err != nil {
			log.Println("logstash:", err)
			continue
		}
		js = append(js, "\n"...) // append a newline to the marshaled JSON
		_, err = a.conn.Write(js)
		if err != nil {
			log.Println("logstash:", err)
			continue
		}
	}
}

// LogstashMessage is a simple JSON input to Logstash.
type LogstashMessage struct {
	Message  string            `json:"message"`
	Name     string            `json:"docker.name"`
	ID       string            `json:"docker.id"`
	Image    string            `json:"docker.image"`
	Hostname string            `json:"docker.hostname"`
	Args     []string          `json:"docker.args,omitempty"`
	Options  map[string]string `json:"options,omitempty"`
}
