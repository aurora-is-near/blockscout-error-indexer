package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configFile, databaseURL, rpcUrl string
var debug bool
var fromBlock, toBlock uint64

type Result struct {
	Error  string `cbor:"error" json:"error"`
	Output string `cbor:"output" json:"output"`
}

func main() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "config file (default is config/local.yaml)")
	rootCmd.PersistentFlags().StringVarP(&rpcUrl, "rpc", "r", "", "rpc url")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "enable debugging(default false)")
	rootCmd.PersistentFlags().StringVar(&databaseURL, "database", "", "database url (default postgres://aurora:aurora@database/aurora)")
	rootCmd.PersistentFlags().Uint64VarP(&fromBlock, "fromBlock", "f", 0, "block to start from. Ignored if missing or 0. (default 0)")
	rootCmd.PersistentFlags().Uint64VarP(&toBlock, "toBlock", "t", 0, "block to end on. Ignored if missing or 0. (default 0)")
	cobra.CheckErr(rootCmd.Execute())
}

func initConfig() {

	if configFile != "" {
		log.Warn().Msg(fmt.Sprint("Using config file:", viper.ConfigFileUsed()))
		viper.SetConfigFile(configFile)
	} else {
		viper.AddConfigPath("config")
		viper.AddConfigPath("../../config")
		viper.SetConfigName("local")
		if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
			panic(fmt.Errorf("Flags are not bindable: %v\n", err))
		}
	}
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err == nil {
		log.Warn().Msg(fmt.Sprint("Using config file:", viper.ConfigFileUsed()))
	}

	debug = viper.GetBool("debug")
	databaseURL = viper.GetString("database")
	fromBlock = viper.GetUint64("fromBlock")
	toBlock = viper.GetUint64("toBlock")
	rpcUrl = viper.GetString("rpc")
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
}

var rootCmd = &cobra.Command{
	Use:     "indexer",
	Short:   "Queries debug_traceTransaction for revert status and a reason",
	Long:    "Queries debug_traceTransaction for revert status and a reason",
	Version: "0.0.1",
	Run: func(cmd *cobra.Command, args []string) {
		pgpool, err := pgxpool.Connect(context.Background(), databaseURL)
		if err != nil {
			panic(fmt.Errorf("Unable to connect to database %s: %v\n", databaseURL, err))
		}
		defer pgpool.Close()

		client, err := rpc.DialContext(context.Background(), rpcUrl)
		if err != nil {
			panic(fmt.Errorf("Unable to connect to %s: %v\n", rpcUrl, err))
		}
		defer client.Close()

		go indexTransactions(pgpool, client, fromBlock, toBlock)

		interrupt := make(chan os.Signal, 10)
		signal.Notify(interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGABRT, syscall.SIGINT)

		<-interrupt
		os.Exit(0)
	},
}

func indexTransactions(pgpool *pgxpool.Pool, client *rpc.Client, fromBlock uint64, toBlock uint64) {
	for {
		hashes := make([]string, 0)
		err := pgpool.QueryRow(context.Background(),
			"SELECT array_agg(hash::varchar) as hashes FROM transactions WHERE status = 0 AND error IS NULL LIMIT 1000").Scan(&hashes)
		if err != nil {
			continue
		}

		if len(hashes) == 0 {
			wait()
			log.Debug().Msg("Waiting for new transactions...")
			continue
		}

		for _, txHash := range hashes {
			var resp Result
			if err := client.Call(&resp, "debug_traceTransaction", strings.Replace(txHash, "\\", "0", 1)); err != nil {
				log.Debug().Msg(fmt.Sprintf("Unable import errors for %v: %v\n", txHash, err))
				updateTx(pgpool, txHash, goqu.Record{"error": "Unknown"})
				continue
			}
			hexdata, err := hex.DecodeString(resp.Output[2:])
			if err != nil {
				log.Debug().Msg(fmt.Sprintf("Unable decode output(%v) for %v: %v\n", resp.Output, txHash, err))
				updateTx(pgpool, txHash, goqu.Record{"error": "Unknown"})
				continue
			}

			if strings.HasPrefix(resp.Error, "Revert") {
				revertReason, err := abi.UnpackRevert(hexdata)
				if err != nil {
					log.Debug().Msg(fmt.Sprintf("Unable unpack output(%v) for %v: %v\n", hexdata, txHash, err))
					updateTx(pgpool, txHash, goqu.Record{"error": "Reverted", "revert_reason": "Unknown"})
				} else {
					updateTx(pgpool, txHash, goqu.Record{"error": "Reverted", "revert_reason": revertReason})
				}
			} else {
				log.Debug().Msg(fmt.Sprintf("Not a revert(%v) for %v: %v\n", resp.Error, txHash, err))
				updateTx(pgpool, txHash, goqu.Record{"error": resp.Error})
			}
		}
	}
}

func updateTx(pgpool *pgxpool.Pool, txHash string, record goqu.Record) {
	updateSQL, _, _ := goqu.Dialect("postgres").From("transactions").
		Where(goqu.C("hash").Eq(txHash)).Update().Set(record).ToSQL()
	pgpool.Exec(context.Background(), sanitizeForSql(updateSQL))
}

func wait() {
	time.Sleep(200 * time.Millisecond)
}

func sanitizeForSql(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, text)
}
