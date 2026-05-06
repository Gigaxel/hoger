# hoger

`hoger` is a small Go, dependency-free, CLI for inspecting and managing local DNS entries in a hosts file.

## Build

```sh
go build -o hoger .
```

## Usage

```sh
hoger [-hosts PATH] list
hoger [-hosts PATH] lookup HOST [HOST...]
hoger [-hosts PATH] add IP HOST [HOST...]
hoger [-hosts PATH] set HOST IP
hoger [-hosts PATH] remove HOST [HOST...]
```

By default, `hoger` uses `/etc/hosts`. You can target another file with `-hosts PATH` or `HOGER_HOSTS`.
Writing to `/etc/hosts` usually requires `sudo`.

## Examples

```sh
hoger list
hoger lookup api.local
sudo hoger add 127.0.0.1 api.local
sudo hoger set api.local 192.168.1.20
sudo hoger remove api.local
```
