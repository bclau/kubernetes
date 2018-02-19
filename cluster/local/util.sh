#!/usr/bin/env bash
# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Perform preparations required to run e2e tests
function prepare-e2e() {
  echo "Local doesn't need special preparations for e2e tests" 1>&2
}

function detect-master {
  #KUBE_MASTER=localhost
  #KUBE_MASTER_IP=127.0.0.1
  #KUBE_MASTER_URL="http://${KUBE_MASTER_IP}:8080"
  #echo "Using master: $KUBE_MASTER (external IP: $KUBE_MASTER_IP)"
  echo " #### Jumping over this part"
}
