@echo off
go generate .\network\messages\incoming
go generate .\network\messages\outgoing
go build -ldflags="-s -w"
