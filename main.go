package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"

	"gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/yaml.v2"

	"github.com/prometheus/common/log"
)

var (
	configFile    = kingpin.Flag("config.file", "ServiceNow configuration file.").Default("config/servicenow.yml").String()
	listenAddress = kingpin.Flag("web.listen-address", "The address to listen on for HTTP requests.").Default(":9877").String()
	config        Config
	serviceNow    ServiceNow
)

// JSONResponse is the Webhook http response
type JSONResponse struct {
	Status  int
	Message string
}

func webhook(w http.ResponseWriter, r *http.Request) {

	data, err := readRequestBody(r)
	if err != nil {
		log.Errorf("Error reading request body : %v", err)
		sendJSONResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	err = manageIncidents(data, config)

	if err != nil {
		log.Errorf("Error managing incident from alert : %v", err)
		sendJSONResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Returns a 200 if everything went smoothly
	sendJSONResponse(w, http.StatusOK, "Success")
}

// Starts 2 listeners
// - first one to give a status on the receiver itself
// - second one to actually process the data
func main() {
	kingpin.Version(version.Print("alertmanager-webhook-servicenow"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	config = loadConfig(*configFile)
	createSnClient(config)

	log.Info("Starting webhook", version.Info())
	log.Info("Build context", version.BuildContext())

	http.HandleFunc("/webhook", webhook)
	http.Handle("/metrics", promhttp.Handler())

	log.Infof("listening on: %v", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}

func sendJSONResponse(w http.ResponseWriter, status int, message string) {
	data := JSONResponse{
		Status:  status,
		Message: message,
	}
	bytes, _ := json.Marshal(data)

	w.WriteHeader(status)
	_, err := w.Write(bytes)

	if err != nil {
		log.Errorf("Error writing JSON response: %s", err)
	}
}

func readRequestBody(r *http.Request) (template.Data, error) {

	// Do not forget to close the body at the end
	defer r.Body.Close()

	// Extract data from the body in the Data template provided by AlertManager
	data := template.Data{}
	err := json.NewDecoder(r.Body).Decode(&data)

	return data, err
}

func loadConfig(configFile string) Config {
	config := Config{}

	// Load the config from the file
	configData, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	errYAML := yaml.Unmarshal([]byte(configData), &config)
	if errYAML != nil {
		log.Fatalf("Error: %v", errYAML)
	}

	return config
}

func createSnClient(config Config) ServiceNow {
	var err error
	serviceNow, err = NewServiceNowClient(config.ServiceNow.InstanceName, config.ServiceNow.UserName, config.ServiceNow.Password)
	if err != nil {
		log.Fatalf("Error creating the ServiceNow client: %v", err)
	}
	log.Info("ServiceNow config loaded")
	return serviceNow
}

func manageIncidents(data template.Data, config Config) error {

	log.Infof("Alerts: Status=%s, GroupLabels=%v, CommonLabels=%v", data.Status, data.GroupLabels, data.CommonLabels)

	for _, alert := range data.Alerts {
		incident := alertToIncident(alert)
		response, err := serviceNow.CreateIncident(incident)

		log.Debugf("Response %s", response)
		if err != nil {
			log.Errorf("Error while creating incident: %v", err)
			return err
		}
	}

	return nil
}

func alertToIncident(alert template.Alert) Incident {
	incident := Incident{
		AssignmentGroup:  alert.Labels["assignment_group"],
		ContactType:      "Monitoring System",
		CallerID:         "Prometheus",
		Description:      alert.Annotations["description"],
		Impact:           "4",
		ShortDescription: alert.Annotations["summary"],
		Urgency:          "3",
	}
	return incident
}
