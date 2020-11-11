#!/bin/bash
MONITORING_NAMESPACE="monitoring"

helm uninstall prometheus \
  --namespace ${MONITORING_NAMESPACE}