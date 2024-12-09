# svart-dns
A Next Generation DNS Sinkhole

This project was created by a long-time user (and admirer) of AdGuard and Pi-Hole. There were just enough features I wish I had that I decided to build my own as a learning exercise and fun challenge.

## Planned Features
* Better visualization of DNS usage and trends
* Load balancing of custom domains to multiple IP addresses
* "What-if" analysis for blocklist changes before applying them
* User-friendly DNS client identification (name your devices!)
* Multi-level rule application (user/group/IP range)
* HTTPS dashboard by default (with automatic self-signed cert generation)
* Support for UDP, TCP, TLS, and DNS-over-HTTPS
* Intelligent blocklist management and comparison
* High performance with minimal resource usage

## Project Status
As of December 8th 2024, this is under heavy active development and is intended only for developers at this time. As the project matures and stabilizes, it will transition to a more user-friendly "download and run" experience.

## Technical Overview
svart-dns is written in Go and designed to be:
* Fast - Optimized for home and small business use
* Easy to deploy - Single binary distribution
* Resource efficient - Smart caching and memory management
* Delightful to use - Clean, intuitive interface

## Development
Currently in early stages. Interested in contributing? Watch this space for:
* Development documentation
* Architecture details
* Contribution guidelines

## Building from Source
```bash
go mod download
go build
```

## License

MIT

## Name

Svart means black in Swedish and comes from an Album by The Haunted - brutal and uncompromising, just like our approach to blocking unwanted DNS requests. 🤘
