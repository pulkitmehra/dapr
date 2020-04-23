// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.
// ------------------------------------------------------------

package grpc

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/dapr/dapr/pkg/config"
	diag "github.com/dapr/dapr/pkg/diagnostics"
	"github.com/dapr/dapr/pkg/logger"
	auth "github.com/dapr/dapr/pkg/runtime/security"
	grpc_go "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	certWatchInterval         = time.Second * 3
	renewWhenPercentagePassed = 70
)

// Server implements the gRPC transport for a Dapr server.
type Server struct {
	logger             logger.Logger
	register           RegisterServerFn
	config             ServerConfig
	listener           net.Listener
	srv                *grpc_go.Server
	tracingSpec        config.TracingSpec
	authenticator      auth.Authenticator
	renewMutex         sync.Mutex
	signedCert         *auth.SignedCertificate
	tlsCert            tls.Certificate
	signedCertDuration time.Duration
}

// RegisterServerFn is the function to register gRPC services.
type RegisterServerFn func(server *grpc_go.Server) error

// APIServerLogger is the logger for the API server
var APIServerLogger = logger.NewLogger("dapr.runtime.grpc.api")

// InternalServerLogger is the logger for the internal server
var InternalServerLogger = logger.NewLogger("dapr.runtime.grpc.internal")

// NewServer creates a new `Server` which delegates service registration to `register`.
func NewServer(logger logger.Logger,
	register RegisterServerFn,
	config ServerConfig,
	tracingSpec config.TracingSpec,
	authenticator auth.Authenticator) *Server {
	return &Server{
		logger:        logger,
		register:      register,
		config:        config,
		tracingSpec:   tracingSpec,
		authenticator: authenticator,
	}
}

// StartNonBlocking starts a new server in a goroutine
func (s *Server) StartNonBlocking() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%v", s.config.Port))
	if err != nil {
		return err
	}
	s.listener = lis

	server, err := s.getGRPCServer()
	if err != nil {
		return err
	}

	err = s.register(s.srv)
	if err != nil {
		return err
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			s.logger.Fatalf("gRPC serve error: %v", err)
		}
	}()
	return nil
}

func (s *Server) generateWorkloadCert() error {
	s.logger.Info("sending workload csr request to sentry")
	signedCert, err := s.authenticator.CreateSignedWorkloadCert(s.config.AppID)
	if err != nil {
		return fmt.Errorf("error from authenticator CreateSignedWorkloadCert: %s", err)
	}
	s.logger.Info("certificate signed successfully")

	tlsCert, err := tls.X509KeyPair(signedCert.WorkloadCert, signedCert.PrivateKeyPem)
	if err != nil {
		return fmt.Errorf("error creating x509 Key Pair: %s", err)
	}

	s.signedCert = signedCert
	s.tlsCert = tlsCert
	s.signedCertDuration = signedCert.Expiry.Sub(time.Now().UTC())
	return nil
}

func (s *Server) getMiddlewareOptions() []grpc_go.ServerOption {
	opts := []grpc_go.ServerOption{}

	if s.tracingSpec.Enabled {
		s.logger.Infof("enabled tracing grpc middleware")
		opts = append(
			opts,
			grpc_go.StreamInterceptor(diag.TracingGRPCMiddlewareStream(s.tracingSpec)),
			grpc_go.UnaryInterceptor(diag.TracingGRPCMiddlewareUnary(s.tracingSpec)))
	}

	s.logger.Infof("enabled metrics grpc middleware")
	opts = append(opts, grpc_go.StatsHandler(diag.DefaultGRPCMonitoring.ServerStatsHandler))

	return opts
}

func (s *Server) getGRPCServer() (*grpc_go.Server, error) {
	opts := s.getMiddlewareOptions()

	if s.authenticator != nil {
		err := s.generateWorkloadCert()
		if err != nil {
			return nil, err
		}

		tlsConfig := tls.Config{
			ClientCAs:  s.signedCert.TrustChain,
			ClientAuth: tls.RequireAndVerifyClientCert,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				return &s.tlsCert, nil
			},
		}
		ta := credentials.NewTLS(&tlsConfig)

		opts = append(opts, grpc_go.Creds(ta))
		go s.startWorkloadCertRotation()
	}

	return grpc_go.NewServer(opts...), nil
}

func (s *Server) startWorkloadCertRotation() {
	s.logger.Infof("starting workload cert expiry watcher. current cert expires on: %s", s.signedCert.Expiry.String())

	ticker := time.NewTicker(certWatchInterval)

	for range ticker.C {
		s.renewMutex.Lock()
		renew := shouldRenewCert(s.signedCert.Expiry, s.signedCertDuration)
		if renew {
			s.logger.Info("renewing certificate: requesting new cert and restarting gRPC server")

			err := s.generateWorkloadCert()
			if err != nil {
				s.logger.Errorf("error starting server: %s", err)
			}
			diag.DefaultMonitoring.MTLSWorkLoadCertRotationCompleted()
		}
		s.renewMutex.Unlock()
	}
}

func shouldRenewCert(certExpiryDate time.Time, certDuration time.Duration) bool {
	expiresIn := certExpiryDate.Sub(time.Now().UTC())
	expiresInSeconds := expiresIn.Seconds()
	certDurationSeconds := certDuration.Seconds()

	percentagePassed := 100 - ((expiresInSeconds / certDurationSeconds) * 100)
	return percentagePassed >= renewWhenPercentagePassed
}
