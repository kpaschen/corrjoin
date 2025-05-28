import argparse
import datetime
import h5py
import numpy as np
import pyarrow.compute as pc
import pyarrow.parquet as pq
from scipy.sparse import dok_matrix

import os
import re
import sys

def convert_metric_to_string(metricName, labels):
  ret = f"name: {metricName}"
  if labels is not None:
    for k, v in labels:
       ret = ret +f", {k}: {v}"
  return ret

if __name__ == "__main__":
   parser = argparse.ArgumentParser("Convert parquet files to hdf5")
   parser.add_argument("--inputfile", default=None, help='Input file (parquet)')  
   parser.add_argument("--outputfile", default=None, help='Where to write the output')

   # You can override these from the commandline, otherwise they will be parsed out of the
   # input filename if possible.
   parser.add_argument("--timeWindow", default=None, help='Id of time window')
   parser.add_argument("--timeFrom", default=None, help='Start time of time window, in YYYYMMDDhhmmss format')
   parser.add_argument("--timeTo", default=None, help='End time of time window, in YYYYMMDDhhmmss format')

   args = parser.parse_args()
   if not os.path.exists(args.inputfile):
      print(f'Missing file {args.inputfile}')
      exit(1)

   timeWindow = args.timeWindow
   timeFrom = args.timeFrom
   timeTo = args.timeTo

   if timeWindow is None or timeFrom is None or timeTo is None:
     filenameparser = re.compile('correlations_(\d+)_(\d+)-(\d+).pq')
     matches = re.match(filenameparser, args.inputfile)
     if matches is None or len(matches.groups()) != 3:
        print(f'missing timewindow, timeFrom and timeTo arguments and failed to parse that information from the input filename {args.inputfile}')
        exit(1)
     timeWindow = timeWindow or matches.groups()[0]
     timeFrom = timeFrom or matches.groups()[1]
     timeTo = timeTo or matches.groups()[2]

   print(f'processing data for time window {timeWindow}, which lasted from {timeFrom} until {timeTo}')

   outputfile = args.outputfile
   if not outputfile:
      print(f"outputfile not specified, writing to {args.inputfile}.hdf5")
   outputfile = args.inputfile + ".hdf5"

   # This file will be closed automatically when the file object goes out of scope.
   hdf5_file = h5py.File(outputfile, 'w')
   hdf5_file.attrs.create(name="timeWindow", data=timeWindow, dtype=int)
   hdf5_file.attrs.create(name="timeFrom", data=timeFrom, dtype=int)
   hdf5_file.attrs.create(name="timeTo", data=timeTo, dtype=int)

   pqfile = pq.ParquetFile(args.inputfile)

   # map metric fingerprint (stable but too large) to metric id (volatile)
   metric_fp_lookup_table = {}

   # Read data for the dimension scale (the metric fingerprints and labels)
   metrics = {}
   ds_columns = ['id', 'metric', 'metricFingerprint', 'labels']
   filter_expr = pc.is_valid(pc.field('labels'))
   for i in range(pqfile.num_row_groups):
      rg = pqfile.read_row_group(i, columns=ds_columns)
      for row in rg.filter(filter_expr).to_pylist():
         ms = convert_metric_to_string(row['metric'], row['labels'])
         metrics[row['id']] = f"fp: {row['metricFingerprint']} metric: {ms}"
         metric_fp_lookup_table[row['metricFingerprint']] = row['id']

   # The dimension scale is a dataset.
   dt = h5py.string_dtype(encoding='utf-8')

   # scipy cannot handle string or object datatypes.
   #sparse_metrics = dok_matrix((max_metric_fingerprint, 1), dtype=object)

   # numpy cannot handle uint64 ranges apparently? That is why I use the volatile id as the 
   # key here instead of the metric fingerprint.
   sparse_metrics = np.full((len(metrics),), "", dtype=object)
   for id, mstring in metrics.items():
     sparse_metrics[id] = mstring
   dimension_dset = hdf5_file.create_dataset(name="metricIds",
     shape=(len(metrics),),
     chunks=True,
     data=sparse_metrics,
     dtype=dt)

   print(f'added dimensions dataset')

   # Read data for the metric status dataset
   status_columns = ['id', 'constant']
   filter_expr = pc.is_valid(pc.field('constant'))
   sparse_metric_info = np.full((len(metrics),), False, dtype=bool)
   for i in range(pqfile.num_row_groups):
      rg = pqfile.read_row_group(i, columns=status_columns)
      for row in rg.filter(filter_expr).to_pylist():
        sparse_metric_info[row['id']] = row['constant']

   status_dset = hdf5_file.create_dataset(name="metricIsConstant",
     chunks=True,
     data=sparse_metric_info)

   print(f'added metric info dataset')

   # Read data for the correlations dataset
   correlation_columns = ['id', 'correlated', 'pearson']
   filter_expr = pc.is_valid(pc.field('pearson'))
   sparse_correlation_data = np.full((len(metrics), len(metrics)), fill_value=0.0, dtype=float)
   for i in range(pqfile.num_row_groups):
     rg = pqfile.read_row_group(i, columns=correlation_columns)
     for row in rg.filter(filter_expr).to_pylist():
        correlated_id = metric_fp_lookup_table[row['correlated']]
        sparse_correlation_data[row['id'], correlated_id] = row['pearson']

   correlation_dset = hdf5_file.create_dataset(name="correlations",
     chunks=True,
     data=sparse_correlation_data)

   hdf5_file.close()
