# Copyright 2022 OpenSSF Scorecard Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM golang:1.24.5@sha256:14fd8a55e59a560704e5fc44970b301d00d344e45d6b914dda228e09f359a088 AS base
WORKDIR /src/scorecard
COPY . ./

FROM base AS build
ARG TARGETOS
ARG TARGETARCH
RUN make build-attestor

FROM gcr.io/google-appengine/debian11@sha256:97dc4fbf18419ef928bcddb865ccf0536d4cc39ae3ace5a2b4273c11aedbea82
COPY --from=build /src/scorecard/attestor /
ENTRYPOINT [ "/scorecard-attestor" ]

