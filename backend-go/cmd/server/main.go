package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"travel-agent/backend-go/internal/api"
	"travel-agent/backend-go/internal/config"
	"travel-agent/backend-go/internal/planner"
	"travel-agent/backend-go/internal/provider/amap"
	"travel-agent/backend-go/internal/provider/llm"
	"travel-agent/backend-go/internal/trace"
)

func main() {
	cfg := config.Load()
	var servicePlanner planner.Planner
	if cfg.Mode == "real" {
		client := llm.NewHTTPClient(cfg.ModelProvider, cfg.ModelBaseURL, cfg.ModelAPIKey, cfg.ModelTimeout)
		servicePlanner = planner.NewLLMPlanner(client, cfg.ModelID, cfg.MaxOutputTokens, cfg.ModelTimeout)
	} else {
		servicePlanner = &planner.FakePlanner{StageDelay: cfg.FakeStageDelay}
	}
	mapClient := amap.NewClient(cfg.AMapAPIKey, cfg.PublicBaseURL)
	traceRepo, err := trace.OpenSQLite(cfg.TraceDBPath)
	if err != nil {
		log.Fatalf("open trace database: %v", err)
	}
	defer traceRepo.Close()
	if fake, ok := servicePlanner.(*planner.FakePlanner); ok {
		fake.TraceRepository = traceRepo
	}
	if llmPlanner, ok := servicePlanner.(*planner.LLMPlanner); ok {
		llmPlanner.TraceRepository = traceRepo
	}
	if llmPlanner, ok := servicePlanner.(*planner.LLMPlanner); ok {
		llmPlanner.POIEnricher = mapClient
	}

	apiServer := api.NewServer(servicePlanner, mapClient, traceRepo)
	apiServer.AllowedOrigins = cfg.AllowedOrigins
	srv := &http.Server{
		Addr:              cfg.Address,
		Handler:           apiServer.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	log.Printf("travel-agent Go server listening on %s (mode=%s, provider=%s)", cfg.Address, cfg.Mode, cfg.ModelProvider)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
