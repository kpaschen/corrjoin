import argparse
import datetime
import h5py
import pandas as pd
import os
import sys

if __name__ == "__main__":
   parser = argparse.ArgumentParser("Convert csv files to hdf5")
   parser.add_argument("--inputfile", default=None, help='Input file (csv)')  
   parser.add_argument("--outputfile", default=None, help='Where to write the output')

   args = parser.parse_args()
   if not os.path.exists(args.inputfile):
      print(f'Missing file {args.inputfile}')
      exit(1)

   outputfile = args.outputfile
   if not outputfile:
      print(f"outputfile not specified, writing to {args.inputfile}.hdf5")
   outputfile = args.inputfile + ".hdf5"

   with open(args.inputfile, 'r', encoding='utf-8') as input_csv:
     df_cols_to_index = input_csv.readline().split(",")

   cols = [ x.strip() for x in df_cols_to_index ]

   store = pd.HDFStore(outputfile)

   # Make sure there is a minimum item size
   item_size = {}
   for chunk in pd.read_csv(args.inputfile, chunksize=5000):
      store.append('hdf_key', chunk, data_columns=cols, index=False, min_itemsize=item_size)

   store.create_table_index('hdf_key', columns=df_cols_to_index, optlevel=9, kind='full')
   store.close()
