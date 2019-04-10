package main

import (
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	fthealth "github.com/Financial-Times/go-fthealth/v1_1"
	log "github.com/Financial-Times/go-logger"
	"github.com/Financial-Times/http-handlers-go/httphandlers"
	"github.com/Financial-Times/public-suggestions-api/service"
	"github.com/Financial-Times/public-suggestions-api/web"
	status "github.com/Financial-Times/service-status-go/httphandlers"
	"github.com/gorilla/mux"
	"github.com/jawher/mow.cli"
	"github.com/rcrowley/go-metrics"
)

const appDescription = "Service serving requests made towards suggestions umbrella"
const suggestPath = "/content/suggest"

func main() {
	app := cli.App("public-suggestions-api", appDescription)

	appSystemCode := app.String(cli.StringOpt{
		Name:   "app-system-code",
		Value:  "public-suggestions-api",
		Desc:   "System Code of the application",
		EnvVar: "APP_SYSTEM_CODE",
	})
	appName := app.String(cli.StringOpt{
		Name:   "app-name",
		Value:  "public-suggestions-api",
		Desc:   "Application name",
		EnvVar: "APP_NAME",
	})
	port := app.String(cli.StringOpt{
		Name:   "port",
		Value:  "8080",
		Desc:   "Port to listen on",
		EnvVar: "APP_PORT",
	})
	authorsSuggestionApiBaseURL := app.String(cli.StringOpt{
		Name:   "authors-suggestion-api-base-url",
		Value:  "http://authors-suggestion-api:8080",
		Desc:   "The base URL to authors suggestion api",
		EnvVar: "AUTHORS_SUGGESTION_API_BASE_URL",
	})
	authorsSuggestionEndpoint := app.String(cli.StringOpt{
		Name:   "authors-suggestion-endpoint",
		Value:  "/content/suggest/authors",
		Desc:   "The endpoint for authors suggestion api",
		EnvVar: "AUTHORS_SUGGESTION_ENDPOINT",
	})
	ontotextSuggestionApiBaseURL := app.String(cli.StringOpt{
		Name:   "ontotext-suggestion-api-base-url",
		Value:  "http://ontotext-suggestion-api:8080",
		Desc:   "The base URL to ontotext suggestion api",
		EnvVar: "ONTOTEXT_SUGGESTION_API_BASE_URL",
	})
	ontotextSuggestionEndpoint := app.String(cli.StringOpt{
		Name:   "ontotext-suggestion-endpoint",
		Value:  "/content/suggest/ontotext",
		Desc:   "The endpoint for ontotext suggestion api",
		EnvVar: "ONTOTEXT_SUGGESTION_ENDPOINT",
	})

	internalConcordancesApiBaseURL := app.String(cli.StringOpt{
		Name:   "internal-concordances-api-base-url",
		Value:  "http://internal-concordances:8080",
		Desc:   "The base URL for internal concordances api",
		EnvVar: "CONCEPT_CONCORDANCES_API_BASE_URL",
	})
	internalConcordancesEndpoint := app.String(cli.StringOpt{
		Name:   "internal-concordances-endpoint",
		Value:  "/internalconcordances",
		Desc:   "The endpoint for internal concordances api",
		EnvVar: "CONCEPT_CONCORDANCES_ENDPOINT",
	})

	publicThingsAPIBaseURL := app.String(cli.StringOpt{
		Name:   "public-things-api-base-url",
		Value:  "http://public-things-api:8080",
		Desc:   "The base URL for public things api",
		EnvVar: "PUBLIC_THINGS_API_BASE_URL",
	})
	publicThingsEndpoint := app.String(cli.StringOpt{
		Name:   "public-things-endpoint",
		Value:  "/things",
		Desc:   "The endpoint for public things api",
		EnvVar: "PUBLIC_THINGS_ENDPOINT",
	})

	conceptBlacklisterBaseUrl := app.String(cli.StringOpt{
		Name:   "concept-blacklister-base-url",
		Value:  "http://concept-suggestions-blacklister:8080",
		Desc:   "The base URL for concept suggester blacklister",
		EnvVar: "CONCEPT_BLACKLISTER_BASE_URL",
	})
	conceptBlacklisterEndpoint := app.String(cli.StringOpt{
		Name:   "concept-blacklister-endpoint",
		Value:  "/blacklist",
		Desc:   "The endpoint for concept suggester blacklister",
		EnvVar: "CONCEPT_BLACKLISTER_ENDPOINT",
	})

	log.InitDefaultLogger(*appName)
	log.Infof("[Startup] public-suggestions-api is starting")

	app.Action = func() {
		log.Infof("System code: %s, App Name: %s, Port: %s", *appSystemCode, *appName, *port)

		tr := &http.Transport{
			MaxIdleConnsPerHost: 128,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		}
		c := &http.Client{
			Transport: tr,
			Timeout:   10 * time.Second,
		}

		authorsSuggester := service.NewAuthorsSuggester(*authorsSuggestionApiBaseURL, *authorsSuggestionEndpoint, c)
		ontotextSuggester := service.NewOntotextSuggester(*ontotextSuggestionApiBaseURL, *ontotextSuggestionEndpoint, c)
		broaderService := service.NewBroaderConceptsProvider(*publicThingsAPIBaseURL, *publicThingsEndpoint, c)

		concordanceService := service.NewConcordance(*internalConcordancesApiBaseURL, *internalConcordancesEndpoint, c)
		blacklister := service.NewConceptBlacklister(*conceptBlacklisterBaseUrl, *conceptBlacklisterEndpoint, c)
		suggester := service.NewAggregateSuggester(concordanceService, broaderService, blacklister, authorsSuggester, ontotextSuggester)
		healthService := NewHealthService(*appSystemCode, *appName, appDescription, authorsSuggester.Check(), ontotextSuggester.Check(), concordanceService.Check(), broaderService.Check(), blacklister.Check())

		serveEndpoints(*port, web.NewRequestHandler(suggester), healthService)

	}
	err := app.Run(os.Args)
	if err != nil {
		log.Errorf("App could not start, error=[%s]\n", err)
		return
	}
}

func serveEndpoints(port string, handler *web.RequestHandler, healthService *HealthService) {

	serveMux := http.NewServeMux()

	serveMux.HandleFunc(healthPath, fthealth.Handler(healthService))
	serveMux.HandleFunc(status.GTGPath, status.NewGoodToGoHandler(healthService.GTG))
	serveMux.HandleFunc(status.BuildInfoPath, status.BuildInfoHandler)

	servicesRouter := mux.NewRouter()
	servicesRouter.HandleFunc(suggestPath, handler.HandleSuggestion).Methods(http.MethodPost)

	var monitoringRouter http.Handler = servicesRouter
	monitoringRouter = httphandlers.TransactionAwareRequestLoggingHandler(log.Logger(), monitoringRouter)
	monitoringRouter = httphandlers.HTTPMetricsHandler(metrics.DefaultRegistry, monitoringRouter)

	serveMux.Handle("/", monitoringRouter)

	server := &http.Server{Addr: ":" + port, Handler: serveMux}

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Infof("HTTP server closing with message: %v", err)
		}
		wg.Done()
	}()

	waitForSignal()
	log.Infof("[Shutdown] public-suggestions-api is shutting down")

	if err := server.Close(); err != nil {
		log.Errorf("Unable to stop http server: %v", err)
	}

	wg.Wait()
}

func waitForSignal() {
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}
