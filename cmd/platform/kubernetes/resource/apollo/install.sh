#!/bin/bash


kubectl exec -i mysql-cluster-master-0 -- mysql -hmysql-cluster -uroot -p < apolloconfigdb.sql
kubectl exec -i mysql-cluster-master-0 -- mysql -hmysql-cluster -uroot -p < apolloportaldb.sql

if [[ $MICRO_ENV == "dev" ]]; then
  srv_overrides="--set configService.replicaCount=1,adminService.replicaCount=1"
  portal_overrides="--set replicaCount=1"
fi

# install the cluster using helm
helm repo add apollo http://ctripcorp.github.io/apollo/charts
helm install -f apollo-portal-values.yaml apollo-portal apollo/apollo-portal  $portal_overrides
helm install -f apollo-service-values.yaml apollo-service apollo/apollo-service  $srv_overrides

