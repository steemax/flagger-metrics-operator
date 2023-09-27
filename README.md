
# Kubernetes flagger-metrics-operator

Создан для автоматического управления базовыми метриками анализа (которые мы считаем обязательными для всех объектов Canary) в рамках Namespace, основываясь на Flagger MetricTemplates.

### Установка

1. Kubectl:
   - cd ./install-manifests; kubectl apply -f ./*
2. Helm:
   - cd ./install-manifests/helm-chart; helm install flagger-template-operatot -f ./values.yaml ./

### Возможности

 1. Следит за MetricTemplates, с фильтром: 
 ```json
 metadata:
   labels:
     base: true
 ```
 2. Создает  ресурс `templates.flagger.3rd.io basic` для автоматического управления параметрами:
 ```json
 - interval: 5m
   thresholdRange:
     max: 58
 ```
   для базовых метрик анализа (базовыми метриками анализа, считаются метрики на основе первого пункта.
 
 3. Следит за ресурсами Canary (все операции кроме удаления), чтобы всегда держать в  актуальном состоянии параметры анализа, в части базовых метрик анализа
 
 ## Механизм работы
 Оператор при старте создает 3 контроллера для слежения за объектами Kubernetes (с помощью штатного механизма подписки на события) - metrictemplate, template, canary
 Все они работают в рамках одного пода оператора flagger-operator-template-xxxxxxxx (при докручивании механихма leader election будет апдейт, для скейла количества подов в большую сторону, что в целом не критично для реализованного внутри оператора функционала)
 

> "2023-09-20T19:32:39Z  INFO  Starting workers  {"controller": "metrictemplate", "controllerGroup": "flagger.app", "controllerKind": "MetricTemplate", "worker count": 1}"

> "2023-09-20T19:32:39Z  INFO  Starting workers  {"controller": "template", "controllerGroup": "flagger.3rd.io", "controllerKind": "Template", "worker count": 1}"

> "2023-09-20T19:32:39Z  INFO  Starting workers  {"controller": "canary", "controllerGroup": "flagger.app", "controllerKind": "Canary", "worker count": 1}"

 #### metrictemplate
 

 - Подписывается на события группы “flagger.app”, “MetricTemplate”
 - При событии с объектом в группе, если этот объект имеет признак базового (в нашем случае label: base: true) анализирует все Canary ресурсы в namespace в котором произошло событие с объектом MetricTemplate
 - В случае если это новый MetricTemplate (создание) и имеет признак базового, то контроллер добавит метрики анализа во все Canary ресурсы указывающие на этот новый объект MetricTemplate
 - В случае если это операция удаления и удаляемый объект MetricTemplate имел признак базового, то контроллер в рамках этого Namespace где произошло событие, очистит метрики анализа для этого MetricTemplate из Canary этого namespace

#### template

 - Подписывается на события ресурса `templates.flagger.3rd.io basic` 
 - Сам ресурс необходим для контроля двух параметров анализа у базовых метрик во всех объектах Canary, параметры указываются для namespace (interval, thresholdRange)
 - Стремится всегда синхронизировать значения этих параметров для базовых метрик анализа в ресурсах Canary, их изменение будет возможно только через правку `templates.flagger.3rd.io basic`
 - Позволяет управлять как всеми Namespace кластера (при наличии прав) так и отдельно взятым Namespace
 - Ресурс `templates.flagger.3rd.io basic` также используется контроллером **MetricTemplate** при добавлении метрик анализа, в части двух вышеперечисленных параметров (interval, thresholdRange)

#### canary

 - Подписывается на события группы "flagger.app", "Canary"
 - При событии с объектом (обновление, создание, но НЕ удаление) проводит анализ этого объекта Canary, на предмет его соответствия эталону (т.е. если в Namespace есть базовые метрики, MetricTemplate с label: base: true) то контроллер проверит что все эти метрики добавлены в анализ и их параметры соответствуют тому что указано для Namespace где произошло события с Canary, в ресурсе `templates.flagger.3rd.io basic`
 - Фактически запрещает ручное изменение параметров анализа для базовых метрик в Canary, всегда стремясь привести их к ожидаемому виду в части анализа

## Конфигурация templates.flagger.3rd.io

    это новый ресурс, написаный нами, для контроля параметров 
   ```
 - interval: 5m
     thresholdRange:
       max: 58
 ```
 

`содержимое ресурса описано в его CRD, и синтаксис контролируется Kubernetes, т.е. в случае не верного наполнения ресурс не получится применить, ниже представлен его полный манифест`

```json
apiVersion: flagger.3rd.io/v1
kind: Template
metadata:
  creationTimestamp: "2023-09-19T16:26:12Z"
  generation: 5
  labels:
    app.kubernetes.io/created-by: flagger-metrics-operator
    app.kubernetes.io/instance: template-sample
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/name: template
    app.kubernetes.io/part-of: flagger-metrics-operator
  name: basic
  namespace: default
  resourceVersion: "64841"
  uid: b89d4797-7378-496f-9845-7ba86b1c017e
spec:
  namespaces:
  - name: default
    metricTemplates:
    - name: nginx-template-testing
      interval: 2m
      thresholdRange:
        max: 5
    - name: new-testing
      interval: 1m
      thresholdRange:
        max: 60
 ```
Добавление полей или изменение их именования - приведет к тому что синтаксис не пройдет валидацию и манифест не будет применен к кластеру. Как видно из примера, конфигурация может быть описана для каждого Namespace в кластере или только для одного. В зависимости от прав (RBAC) для Service Account из под которого запущен оператор, можно управлять  настройками как для всех Namespace так и только для одного. 

## Пример записываемого лога
```log
2023/09/25 18:54:30 Updater: Update cache information about MetricTemplates in cluster scope:
2023/09/25 18:54:30 Updater: NameTpl: new-testing, Namespace: default, LabelHave: true
2023/09/25 18:54:30 Updater: NameTpl: nginx-template-testing, Namespace: default, LabelHave: true
2023/09/25 18:54:30 CanaryReconciler: Detect Modify request for Canary : nginx-deployment, in namespace: default, sleep 10 seconds...
2023/09/25 18:54:30 TemplateReconciler: Detect Modify request for templates.flagger.3rd.io :basic, in namespace: default
2023/09/25 18:54:30 MetricTemplateReconciler: Detect Update or Create request for flagger metricTemplate: new-testing, in namespace: default
2023/09/25 18:54:30 MetricTemplateReconciler: Modified metricTemplate has BASE label, updating Canary resources
2023/09/25 18:54:30 MetricTemplateReconciler: match found for triggeres MetricTemplate with templates.flagger.3rd.io basic, is the metric name: new-testing namespace: default
2023/09/25 18:54:30 TemplateReconciler: No need update for Canary nginx-deployment (new-testing), skip updating...
2023/09/25 18:54:30 TemplateReconciler: No need update for Canary nginx-deployment (nginx-template-testing), skip updating...
2023/09/25 18:54:30 MetricTemplateReconciler: No need update for Canary nginx-deployment (new-testing)  skip updating...
2023/09/25 18:54:30 MetricTemplateReconciler: Detect Update or Create request for flagger metricTemplate: nginx-template-testing, in namespace: default
2023/09/25 18:54:30 MetricTemplateReconciler: Modified metricTemplate has BASE label, updating Canary resources
2023/09/25 18:54:30 MetricTemplateReconciler: match found for triggeres MetricTemplate with templates.flagger.3rd.io basic, is the metric name: nginx-template-testing namespace: default
2023/09/25 18:54:30 MetricTemplateReconciler: No need update for Canary nginx-deployment (nginx-template-testing)  skip updating...
2023/09/25 18:54:30 MetricTemplateReconciler: Detect Update or Create request for flagger metricTemplate: metric-nginx-template, in namespace: default
2023/09/25 18:54:30 MetricTemplateReconciler: Updated MetricTemplate metric-nginx-templatein namespace: default NOT base metric template, skipping update Canary Analisys...

2023/09/25 18:54:40 CanaryReconciler: Canary nginx-deployment in Namespace default modified, now check base MetricTemplate is correct...
2023/09/25 18:54:40 CanaryReconciler: No need update for Canary nginx-deployment (new-testing), skip updating...
2023/09/25 18:54:40 CanaryReconciler: No need update for Canary nginx-deployment (nginx-template-testing), skip updating...

2023/09/25 18:55:31 Updater: Update cache information about MetricTemplates in cluster scope:
2023/09/25 18:55:31 Updater: NameTpl: new-testing, Namespace: default, LabelHave: true
2023/09/25 18:55:31 Updater: NameTpl: nginx-template-testing, Namespace: default, LabelHave: true
2023/09/25 18:55:52 MetricTemplateReconciler: Detect Update or Create request for flagger metricTemplate: new-testing-1, in namespace: default
2023/09/25 18:55:52 MetricTemplateReconciler: Modified metricTemplate has BASE label, updating Canary resources
2023/09/25 18:55:52 MetricTemplateReconciler: You need add description for MetricTemplate name: new-testing-1 to templates.flagger.3rd.io basic, now this base metric template is available, but not described (interval and threshhold range), skip update in Canary

2023/09/25 18:56:31 Updater: Update cache information about MetricTemplates in cluster scope:
2023/09/25 18:56:31 Updater: NameTpl: new-testing, Namespace: default, LabelHave: true
2023/09/25 18:56:31 Updater: NameTpl: new-testing-1, Namespace: default, LabelHave: true
2023/09/25 18:56:31 Updater: NameTpl: nginx-template-testing, Namespace: default, LabelHave: true```

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

