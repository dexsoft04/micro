#!/bin/bash
MONITORING_NAMESPACE="monitoring"

helm uninstall grafana \
    --namespace ${MONITORING_NAMESPACE}
