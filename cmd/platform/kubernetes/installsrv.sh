cd #!/bin/bash

if [ $MICRO_ENV == "dev" ]; then
  sed -i '' 's/\*.m3o.app/\*.mcbeam.dev/g' 'ls ./service'
  sed -i '' 's/m3o.com/mcbeam.dev/g' ingress.yaml
fi

if [ $MICRO_ENV == "dev" ]; then
  sed -i '' 's/ccr.ccs.tencentyun.com\/wolfplus\/mcbeam/ccr.ccs.tencentyun.com\/wolfplus\/mcbeam:mcbeam-v3-202011071039/g'
  sed -i '' '/containers:/i\\      imagePullSecrets:\n      - name: qcloudregistrykey'

kubectl apply -f service

win:
  sed -i '' 's/mcbeam-v3-202011191305/mcbeam-v3-202011221721/g' `ls ./service/*`
