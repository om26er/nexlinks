# NexLinks
This project implements router-to-router links on top of Nexus WAMP router. The feature is not part of the WAMP protocol,
however something that Crossbar.io implements. The expectation is that it will eventually make it into the WAMP spec.

The approach is to connect any number of WAMP routers to each other. This project currently implements forwarding RPCs
and Events from an Edge node to cloud. That means Nexus running on the Edge and connected to Crossbar on the cloud.

One other aim is to show how capable Nexus is and how easily extensible it is, due to the fact that the WAMP router
is written as a library and extending that is super easy and fun.

## Build from source
```shell
git clone git@github.com:om26er/nexlinks.git
cd nexlinks
make build
./router
```
To actually test the RPC/Event forwarding capabilities of this project, it is required to run Crossbar as well.
We don't include the instructions to run Crossbar, however we do provide the required config for it in the
`crossbar` directory.

So given one have Crossbar already installed on a computer, you can just run below command and you should have
router-to-router links working. Albeit as a proof of concept.
```shell
make run_crossbar
```
