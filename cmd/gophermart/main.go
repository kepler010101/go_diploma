package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"gophermart/handlers"
	"gophermart/repo"
	"gophermart/service"
)

const defaultTokenSecret = "gofirmart-secret"

type Config struct {
	RunAddress           string
	DatabaseURI          string
	AccrualSystemAddress string
	AccrualPollInterval  time.Duration
	AccrualWorkers       int
	TokenSecret          string
}

func main() {
	cfg := LoadConfig()

	baseCtx := context.Background()

	db, err := repo.NewDB(baseCtx, cfg.DatabaseURI)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Printf("close database: %v", cerr)
		}
	}()

	if err := repo.Migrate(baseCtx, db); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	userRepo := repo.NewUserRepository(db)
	orderRepo := repo.NewOrderRepository(db)

	accrualClient := service.NewAccrualClient(cfg.AccrualSystemAddress)
	workerPool := service.NewWorkerPool(orderRepo, accrualClient, cfg.AccrualWorkers, cfg.AccrualPollInterval)

	authSvc := service.NewAuthManager(userRepo, cfg.TokenSecret)
	orderSvc := service.NewOrderManager(orderRepo)
	balanceSvc := service.NewBalanceManager(userRepo)
	withdrawRepo := repo.NewWithdrawRepository(db)
	withdrawSvc := service.NewWithdrawManager(withdrawRepo)

	router := handlers.NewRouter(authSvc, orderSvc, balanceSvc, withdrawSvc)

	server := &http.Server{
		Addr:         cfg.RunAddress,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
	workerPool.Start(ctx)

	go func() {
		<-ctx.Done()
		log.Printf("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	log.Printf("GoFirmart is listening on %s", cfg.RunAddress)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}

	stop()
	workerPool.Wait()

	log.Println("GoFirmart stopped")
}

func LoadConfig() Config {
	cfg := Config{
		RunAddress:          ":8080",
		AccrualPollInterval: 2 * time.Second,
		AccrualWorkers:      3,
		TokenSecret:         defaultTokenSecret,
	}

	applyEnv(&cfg)
	applyFlags(&cfg)

	return cfg
}

func applyEnv(cfg *Config) {
	if v, ok := os.LookupEnv("RUN_ADDRESS"); ok && v != "" {
		cfg.RunAddress = v
	}

	if v, ok := os.LookupEnv("DATABASE_URI"); ok && v != "" {
		cfg.DatabaseURI = v
	}

	if v, ok := os.LookupEnv("ACCRUAL_SYSTEM_ADDRESS"); ok && v != "" {
		cfg.AccrualSystemAddress = v
	}

	if v, ok := os.LookupEnv("ACCRUAL_POLL_INTERVAL"); ok && v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			cfg.AccrualPollInterval = time.Duration(seconds) * time.Second
		} else {
			log.Printf("invalid ACCRUAL_POLL_INTERVAL value %q: %v", v, err)
		}
	}

	if v, ok := os.LookupEnv("ACCRUAL_WORKERS"); ok && v != "" {
		if workers, err := strconv.Atoi(v); err == nil && workers > 0 {
			cfg.AccrualWorkers = workers
		} else {
			log.Printf("invalid ACCRUAL_WORKERS value %q: %v", v, err)
		}
	}
}

func applyFlags(cfg *Config) {
	pollDefault := int(cfg.AccrualPollInterval / time.Second)
	if pollDefault <= 0 {
		pollDefault = 2
	}

	runAddress := flag.String("a", cfg.RunAddress, "server listen address")
	databaseURI := flag.String("d", cfg.DatabaseURI, "database connection URI")
	accrualAddress := flag.String("r", cfg.AccrualSystemAddress, "accrual system address")
	accrualPoll := flag.Int("p", pollDefault, "accrual poll interval in seconds")
	accrualWorkers := flag.Int("w", cfg.AccrualWorkers, "accrual workers count")

	flag.Parse()

	cfg.RunAddress = *runAddress
	cfg.DatabaseURI = *databaseURI
	cfg.AccrualSystemAddress = *accrualAddress

	if *accrualPoll > 0 {
		cfg.AccrualPollInterval = time.Duration(*accrualPoll) * time.Second
	}

	if *accrualWorkers > 0 {
		cfg.AccrualWorkers = *accrualWorkers
	}
}
