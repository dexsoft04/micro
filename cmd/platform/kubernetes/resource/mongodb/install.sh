#!/bin/bash

SIZE=$1

# default to 25Gi
if [ "$SIZE" == "" ]; then
  SIZE=10Gi
fi

if [[ $MICRO_ENV == "dev" ]]; then
  overrides="--set persistence.size=$SIZE"
fi

# install the cluster using helm
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install mongodb-cluster bitnami/mongodb $overrides -f values.yaml
