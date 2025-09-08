# [call.zip](https://call.zip)

Stress test large video calls using lightweight viewer bots.  
Compatible with Jicofo and JVB (using VP9 SVC codec) to test Jitsi Meet conferences over LAN.

### Getting started

Launch 100 viewer bots with:

```bash
go run main.go -room test1 -n 100 -server-ip x.x.x.x -client-ip y.y.y.y
```

[Go 1.24 or newer](https://go.dev) is required.

### Contribution

Created by [Evan Ram](https://linkedin.com/in/evanram) in the [Internet Systems Lab](https://netstech.org) at the [University of Colorado Boulder](https://colorado.edu).

### Legal Disclaimer

This software is intended solely for legitimate testing and research purposes.

The authors and distributors of call.zip do not condone or support any use of this tool to conduct denial‑of‑service attacks, disrupt services, or otherwise violate the laws of any jurisdiction.  Users are responsible for ensuring that their use complies with all applicable regulations and the terms of service of the target system.

By downloading, building, or running call.zip, you acknowledge that you understand these restrictions and agree to use the software only in lawful and authorized contexts.