#!/bin/bash

if [ $MICRO_ENV == "dev" ]; then
  sed -i '' 's/\*.m3o.app/\*.mcbeam.dev/g' 'ls ./service'
  sed -i '' 's/m3o.com/mcbeam.dev/g' ingress.yaml
fi

if [ $MICRO_ENV == "dev" ]; then
  sed -i '' 's/ghcr.io\/m3o\/platform/ccr.ccs.tencentyun.com\/wolfplus\/mcbeam/g' 'ls ./service'
  sed -i '' 's/m3o.com/mcbeam.dev/g' 'ls ./service'
fi

kubectl apply -f service