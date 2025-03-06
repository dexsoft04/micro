#!/bin/bash

SIZE=$1

# default to 25Gi
if [ "$SIZE" == "" ]; then
  SIZE=10Gi
fi

if [[ $MICRO_ENV == "dev" ]]; then
  overrides=""
fi

# install the cluster using helm
helm repo add bitnami https://charts.bitnami.com/bitnami
helm install mysql-cluster bitnami/mysql --version 9.1.4 $overrides \
  --set master.persistence.size=$SIZE,slave.persistence.size=$SIZE \
  -f values.yaml
