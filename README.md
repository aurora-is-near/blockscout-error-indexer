# Blockscout Error Indexer

Blockscout Error Indexer used to continuously populate blockscout postgres database with errors for failed transactions. It relies debug_traceTransaction.

## How to build
```bash
go build #mac
make #linux
```

## Template for config.yaml:
```yaml
---
database: postgres://aurora:aurora@database/aurora
rpc: https://mainnet.aurora.dev
debug: false
fromBlock: 0 #TODO
toBlock: 0 #TODO
```

## How to use

```bash
Usage:
  indexer [flags]

Flags:
  -c, --config string         config file (default is config/local.yaml)
      --database string       database url (default is postgres://aurora:aurora@database/aurora)
  -r, --rpc string         config file (default is config/local.yaml)
  -d, --debug                 enable debugging(default is false)
  -f, --fromBlock uint        block to start from. Ignored if missing or 0 (default is 0)
  -h, --help                  help for indexer
  -t, --toBlock uint          block to end on. Ignored if missing or 0 (default is 0)
  -v, --version               version for indexer
```

## Example of usage

```bash
./indexer # Using config from `config/local.yaml`
./indexer --config config/mainnet.yaml # Using different config file
```
