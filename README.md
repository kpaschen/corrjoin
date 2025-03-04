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
in the file system as .json files. This is just a caching mechanism because the Parquet files
can be quite large.

The explorer provides a REST API endpoint that serves json data and I use it as a backend
for Grafana visualizations. 

## Getting started

Run the unit tests:

`go test ./...`

Build the docker container:

`docker build -f Dockerfile-receiver -t receiver:v0.1 .`

Bring up the system in a local kind cluster:

`(cd setup && ./setup-local.sh)`

If this fails:
- make sure `kind` is on your path
- login to ghcr.io like this:

 `echo $tok | docker login ghcr.io -u $your_github_username --password-stdin`

where `tok` is a github access token.

## Running in a Kubernetes cluster

You can run the system outside of a Kubernetes cluster; all you need is a Prometheus instance sending data
to wherever your receiver process is running. But a Kubernetes cluster makes this easy to set up, and it is
a great source of timeseries data.

I run this in a cluster set up with _kubespray_, but really any cluster will do.
You should have persistent storage available on at least one worker node.
The receiver + explorer need about 4-6GB of memory even in a smallish cluster.

The script in `setup/install.sh` has the steps I go through for setting up a cluster. Note a few steps are
commented out; you should take a look and see e.g. whether you have rook/ceph or some other storage provider
and make changes accordingly.

The configuration for Prometheus and Grafana is in `prometheus-values.yaml`. This is just a values file for
the kube-prometheus-stack helm chart.

There are two custom Grafana dashboards in `helm/corrjoin/templates` that you can adapt.

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

For the systems I have experimented with (30-60k timeseries, correlation threshold 0.90-0.99, row group size 10k),
the parquet files are 0.5-1.2GB in size. If you write one every half hour, you're using 1-2GB of disk space per hour. I usually keep only the latest 10 parquet files around, so 30GB of disk space should be enough.

## Getting value out of the data

### Pruning the list of time series

Some time series are poor targets for correlation analysis; if you know about such series, it helps if you
drop them before processing.

The sample prometheus-values.yaml file has a remote write configuration that lets you do that.

### Exploring the data

The corrjoin helm chart has a Grafana dashboard called `Stride Exploration` that lets you browse the available
data by time window (stride). 

You can think of the correlation results as a temporal graph. There is a version of the graph at every stride.
The nodes in the graph for a stride are time series names (aka _metrics_). 
There is an edge between two metrics if they are correlated. The label on the edge is the Pearson correlation
coefficient. We only represent edges that are above the configured threshold.

When you look at the data for a stride, you can see a list of _subgraphs_. Basically, the graph of metrics for a
stride is usually not connected, but it consists of subgraphs that are not connected with each other. Often,
there will be a lot of subgraphs with two or three nodes in them, and a few subgraphs with a lot (maybe several thousand) nodes.

You can select a subgraph to list the metrics in it, and then select metrics that you want to see e.g. timeseries
graphs for.

### How is this useful?

I am still working out the best way to present the data, but I think I'd use it like this:

I usually have a few metrics for things with user-visible impact, like latency and error rates on a web service.
It would probably be interesting to see which other time series are correlated with these "leading" metrics, perhaps
also to see when the correlation appears or disappears.

For example, let's say I discover that my web service's error rate becomes correlated with my database's latency
when there are a lot of requests against the web service. That would be valuable information. Similarly, if I know
my service's latency is not correlated with the cpu load on my nodes, that is also good to know.



