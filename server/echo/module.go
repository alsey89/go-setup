package echo_module

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

const (
	DefaultHost     = "0.0.0.0"
	DefaultLogLevel = "DEV"
	DefaultPort     = 3001
)

type Config struct {
	AllowHeaders string
	AllowMethods string
	AllowOrigins string
	Host         string
	LogLevel     string
	Port         int
}

type HTTPServer struct {
	config *Config
	logger *zap.Logger
	scope  string
	server *echo.Echo
}

type Params struct {
	fx.In

	Lifecycle fx.Lifecycle
	Logger    *zap.Logger
}

func InitiateModule(scope string) fx.Option {
	return fx.Module(
		scope,
		fx.Provide(func(p Params) *HTTPServer {
			logger := p.Logger.Named("[" + scope + "]")
			server := echo.New()
			config := loadConfig(scope)

			s := &HTTPServer{
				logger: logger,
				scope:  scope,

				config: config,
				server: server,
			}

			return s
		}),
		fx.Invoke(func(s *HTTPServer, p Params) {
			p.Lifecycle.Append(
				fx.Hook{
					OnStart: s.onStart,
					OnStop:  s.onStop,
				},
			)
		}),
	)
}

func loadConfig(scope string) *Config {
	//set defaults
	viper.SetDefault(fmt.Sprintf("%s.allow_headers", scope), "Origin,Content-Type,Accept")
	viper.SetDefault(fmt.Sprintf("%s.allow_methods", scope), "GET,PUT,POST,DELETE")
	viper.SetDefault(fmt.Sprintf("%s.allow_origins", scope), "*")

	viper.SetDefault(fmt.Sprintf("%s.host", scope), DefaultHost)
	viper.SetDefault(fmt.Sprintf("%s.log_level", scope), DefaultLogLevel)
	viper.SetDefault(fmt.Sprintf("%s.port", scope), DefaultPort)

	getConfigPath := func(key string) string {
		return fmt.Sprintf("%s.%s", scope, key)
	}

	return &Config{
		AllowHeaders: viper.GetString(getConfigPath("allow_headers")),
		AllowMethods: viper.GetString(getConfigPath("allow_methods")),
		AllowOrigins: viper.GetString(getConfigPath("allow_origins")),
		Host:         viper.GetString(getConfigPath("host")),
		LogLevel:     viper.GetString(getConfigPath("log_level")),
		Port:         viper.GetInt(getConfigPath("port")),
	}
}

func (s *HTTPServer) onStart(ctx context.Context) error {
	s.logger.Info("HTTPServer initiated")

	s.setUpCorsMiddleware()
	s.setUpRequestLoggerMiddleware()

	go s.startServer(true, true)

	s.PrintDebugLogs()

	return nil
}

func (s *HTTPServer) onStop(context.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.server.Shutdown(ctx)
	if err != nil {
		s.logger.Error("server shutdown error", zap.Error(err))
	}

	s.logger.Info("HTTPServer stopped")
	return nil
}

func (s *HTTPServer) setUpCorsMiddleware() {
	// configure CORS middleware
	corsConfig := middleware.CORSConfig{
		AllowOrigins: strings.Split(s.config.AllowOrigins, ","),
		AllowMethods: strings.Split(s.config.AllowMethods, ","),
		AllowHeaders: strings.Split(s.config.AllowHeaders, ","),
	}
	if s.config.AllowOrigins == "" {
		corsConfig.AllowOrigins = []string{"*"}
	}
	if s.config.AllowMethods == "" {
		corsConfig.AllowMethods = []string{http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete}
	}
	if s.config.AllowHeaders == "" {
		corsConfig.AllowHeaders = []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept}
	}
	// add CORS middleware
	s.server.Use(middleware.CORSWithConfig(corsConfig))
}

func (s *HTTPServer) setUpRequestLoggerMiddleware() {

	// configure request logger according to log level
	requestLoggerConfig := middleware.RequestLoggerConfig{
		LogProtocol:  true,
		LogMethod:    true,
		LogURI:       true,
		LogStatus:    true,
		LogRequestID: true,
		LogRemoteIP:  true,
		LogLatency:   true,
		LogError:     true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			switch s.config.LogLevel {
			case "DEV":
				log.Printf("|--------------------------------------------\n")
				s.logger.Info("request",
					zap.String("URI", v.URI),
					zap.String("method", v.Method),
					zap.Int("status", v.Status),
					zap.Any("error", v.Error),
					zap.String("remote_ip", v.RemoteIP),
					zap.String("request_id", v.RequestID),
					zap.Duration("latency", v.Latency),
					zap.String("protocol", v.Protocol),
				)
				log.Printf("--------------------------------------------|\n")
			case "PROD":
				log.Printf("|--------------------------------------------\n")
				s.logger.Info("request",
					zap.String("URI", v.URI),
					zap.Int("status", v.Status),
					zap.Any("error", v.Error),
					zap.String("request_id", v.RequestID),
					zap.Duration("latency", v.Latency),
				)
				log.Printf("--------------------------------------------|\n")
			case "DEBUG":
				log.Printf("|--------------------------------------------\n")
				s.logger.Debug("request",
					zap.String("URI", v.URI),
					zap.String("method", v.Method),
					zap.Int("status", v.Status),
					zap.String("remote_ip", v.RemoteIP),
					zap.String("request_id", v.RequestID),
					zap.Duration("latency", v.Latency),
					zap.String("protocol", v.Protocol),
					zap.Any("error", v.Error),
					zap.Any("request_body", c.Request().Body),
					// todo: add more debug logs if needed
				)
				log.Printf("--------------------------------------------|\n")
			default:
				s.logger.Error("invalid log level", zap.String("log_level", s.config.LogLevel))
			}
			return nil
		},
	}
	// add request logger middleware
	s.server.Use(middleware.RequestLoggerWithConfig(requestLoggerConfig))
}

func (s *HTTPServer) startServer(HideBanner bool, HidePort bool) {
	s.server.HideBanner = HideBanner || false
	s.server.HidePort = HidePort || false

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	err := s.server.Start(addr)
	if err != nil && err != http.ErrServerClosed {
		s.logger.Fatal(err.Error())
	}
}

func (s *HTTPServer) GetServer() *echo.Echo {
	return s.server
}

func (s *HTTPServer) PrintDebugLogs() {
	//* Debug Logs
	//server
	s.logger.Debug("----- Server Configuration -----")
	s.logger.Debug("Host", zap.String("Host", s.config.Host))
	s.logger.Debug("Port", zap.Int("Port", s.config.Port))
	//cors
	s.logger.Debug("----- Cors Configuration -----")
	s.logger.Debug("AllowOrigins", zap.String("AllowOrigins", s.config.AllowOrigins))
	s.logger.Debug("AllowMethods", zap.String("AllowMethods", s.config.AllowMethods))
	s.logger.Debug("AllowHeaders", zap.String("AllowHeaders", s.config.AllowHeaders))
}