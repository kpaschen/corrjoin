# Correlating Prometheus timeseries

## What does this do?

This is a prototype of a correlation analysis tool for Prometheus time series.

The main component is an application called the "receiver" that receives time series
data via the remote write protocol and computes pearson correlation coefficients for pairs
of timeseries.

The receiver uses sliding time windows of a configurable size. For example, you could have
a window size of 500 with a "stride" length of 100 and a sample time of 20s. This means we 
compute correlation coefficients every 100 * 20 seconds (so roughly every half hour).

When the receiver finds a pair of time series whose Pearson correlation coefficient is above
a configured threshold (typical threshold values are 0.9 - 0.99), it records this information
in a Parquet file. The receiver produces one Parquet file per sliding window.

In order to get value out of these files, there is a second component called the explorer.
This currently runs in the same process as the receiver, but there is no technical requirement
for that, it was just easy.

The explorer scans the directory with the Parquet files. When it finds a Parquet file that it
hasn't read yet, it reads it and extracts some information from it. It stores this information
in the file system as .csv files. This is just a caching mechanism because the Parquet files
can be quite large.

The explorer provides a REST API endpoint that serves json data and I use it as a backend
for Grafana visualizations. 

## Getting started

Bring up a local kind cluster:

`(cd setup && ./setup-local.sh)`

If this fails, make sure `kind` is on your path.

Bring up the receiver + explorer as well as a kube-prometheus-stack.
I publish a docker image at `ghcr.io/kpaschen/corrjoin`. By default, that is 
what will be used, but you can build your own image like this:

`docker build -f Dockerfile-receiver -t receiver:v0.1 .`

If you do that, publish your image to a container registry and make the environment variable
`$IMAGEREGISTRY` point to that registry.

`(cd setup && ./install-corrjoin.sh)`

## Running in a Kubernetes cluster

You can run the system outside of a Kubernetes cluster; all you need is a Prometheus instance sending data
to wherever your receiver process is running. But a Kubernetes cluster makes this easy to set up, and it is
a great source of timeseries data.
The receiver + explorer need about 4GB of memory even in a smallish cluster.

The configuration for Prometheus and Grafana is in `prometheus-values.yaml`. This is just a values file for
the kube-prometheus-stack helm chart.
There are custom Grafana dashboards in `helm/corrjoin/templates` that you can adapt.

The Grafana admin password is stored in a secret and you can configure it by overriding the `grafana.password` value in the file `setup/helm/corrjoin/values-cloud.yaml`.
The helm chart in `setup/helm/corrjoin` sets up the receiver and explorer system.

### Make sure you have enough persistent storage

The corrjoin implementation writes correlation data to Parquet files in a persistent volume. It writes one parquet
file per time window analysed. The size of the parquet file depends on:

- how many timeseries are in your system
- how many of them are correlated with each other
- the maxRowGroup parameter

The first two dependencies are probably self-explanatory; the third one comes about because Parquet file compression
is applied at the row group level, so if you have many small row groups, you have worse compression. On the other
hand, large row groups need more memory, so it's a balance. That's why there is a commandline parameter.

For the systems I have experimented with (30-60k timeseries, correlation threshold 0.90-0.99, row group size 20k),
the parquet files are 0.5-1.2GB in size. If you write one every half hour, you're using 1-2GB of disk space per hour. I usually keep only the latest 10 parquet files around, so 30GB of disk space should be enough.

### Creating more interesting time series

With the above setup, your cluster will be mostly quiet, so you'll have a lot of constant time series.
If you're just testing and need to generate a bit of load, I have a loadtesting setup.

The loadtesting setup installs Mattermost (a chat server) with a Postgres DB and an Apache reverse proxy in the cluster.
The apache reverse proxy is not set up as an Ingress server, and it runs _inside_ the cluster; its main purpose is to
export additional time series.

I then run a loadtesting tool published by the Mattermost team.
All the details are in the setup script in `setup/install-loadtestenv.sh`.
You should probably take a look at the script before running it, since it wants a few parameters to be configured
via environment variables.

If you want more interesting data, there is a `stresstest` pod in the loadtest helm chart. This is not enabled by default.
If you turn it on, it will run on the same node as the mattermost service (via pod affinity) and create some cpu load and
i/o operations there. You can turn the stress up or down by modifying the parameters in the pod template.

## Getting value out of the data

### Pruning the list of time series

Some time series are poor targets for correlation analysis; if you know about such series, it helps if you
drop them before processing.
The sample prometheus-values.yaml file has a remote write configuration that lets you do that.

### Exploring the data

The corrjoin helm chart has a Grafana dashboard called `Explore Strides` that lets you browse the available
data by time window (stride). 

You can think of the correlation results as a temporal graph. There is a version of the graph at every stride.
The nodes in the graph for a stride are time series names (aka _metrics_). 
There is an edge between two metrics if they are correlated. The label on the edge is the Pearson correlation
coefficient. We only represent edges that are above the configured threshold.

When you look at the data for a stride, you can see a list of _subgraphs_. Basically, the graph of metrics for a
stride is usually not connected, but it consists of multiple subgraphs. Often,
there will be a lot of subgraphs with two or three nodes in them, and a few subgraphs with a lot (maybe several thousand) nodes.
You can select a subgraph to list the metrics in it, and then select metrics that you want to see e.g. timeseries
graphs for. Note that the API only returns 150 nodes and the Grafana graph rendering is most useful for below 20 nodes.
I am evaluating better graph visualization options.

There is a second dashboard for a more targeted exploration, and it is called `Explore Metrics`. This lets you select
metrics by their name, so you could look for metrics that track memory usage for example. You can then select one or more
of them via the menu at the top and compare their correlation relation with each other over time.

The `Explore Metrics` dashboard can also consume a time series identifier as a URL parameter. You can use this for example
with Grafana _Panel Links_. The `experiments` dashboard has a Panel at the bottom showing `process_cpu_seconds` metrics. Click on
any of them and you'll get a little popup with a link `Explore correlated timeseries` at the bottom. Clicking on that link
will take you to the `Explore Metrics` dashboard prepopulated with the time series you just clicked on.


### How is this useful?

I am still working out the best way to present the data, but I think I'd use it like this:

I usually have a few metrics for things with user-visible impact, like latency and error rates on a web service.
It would probably be interesting to see which other time series are correlated with these "leading" metrics, perhaps
also to see when the correlation appears or disappears.

For example, let's say I discover that my web service's latency becomes correlated with my database's latency
when there are a lot of requests against the web service. That would be valuable information. Similarly, if I know
my service's latency is not correlated with the cpu load on my nodes, that is also good to know.



