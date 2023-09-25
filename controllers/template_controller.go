/*
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
*/

package controllers

import (
	"context"
	"log"
	"time"

	flaggerv1beta1 "github.com/fluxcd/flagger/pkg/apis/flagger/v1beta1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	flaggerv1 "github.com/steemax/flagger-metrics-operator/api/v1"
	"github.com/steemax/flagger-metrics-operator/updater"
)

// TemplateReconciler reconciles a Template object
type TemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// MetricTemplateReconciler reconciles a MetricTemplate object
type MetricTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

type CanaryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

type AnalysisBasic struct {
	Namespace          string
	MetricTemplateName []MetricTemplateInfo
}

type MetricTemplateInfo struct {
	TemplateName string
	Interval     string
	ThresholdMin float64
}

type StoreData struct {
	Namespace       string
	MetricTemplName string
	HaveLabel       bool
}

var analysisData []AnalysisBasic
var metricTemplates flaggerv1beta1.MetricTemplateList
var canaries flaggerv1beta1.CanaryList

func NewTemplateReconciler(mgr ctrl.Manager) *TemplateReconciler {
	return &TemplateReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Template"),
		Scheme: mgr.GetScheme(),
	}
}

func (r *MetricTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	metricTemplate := &flaggerv1beta1.MetricTemplate{}

	if err := r.Client.Get(ctx, req.NamespacedName, metricTemplate); err != nil {
		if errors.IsNotFound(err) {
			// Обработка удаления объекта
			log.Printf("MetricTemplateReconciler: Received DELETE request for metricTemplate: %s, in namespace: %s", req.Name, req.Namespace)
			return r.handleDeletion(ctx, req.Name, req.Namespace) // Передача req.Name и req.Namespace
		}
		return ctrl.Result{}, err
	}

	// Обработка создания или обновления объекта
	return r.handleCreationOrUpdate(ctx, metricTemplate)
}

func (r *MetricTemplateReconciler) handleDeletion(ctx context.Context, name, namespace string) (ctrl.Result, error) {
	if name == "" || namespace == "" {
		log.Print("MetricTemplateReconciler: Invalid metricTemplate object in handleDeletion")
		return ctrl.Result{}, nil
	}

	log.Print("MetricTemplateReconciler: Detect DELETE request for flagger metricTemplate: ", name, ", in namespace: ", namespace)

	// Проходим по элементам в resultInMemory и ищем совпадение имени и LabelHave
	for _, info := range updater.MetricTemplateInfoList {
		if info.NameTpl == name && info.LabelHave {
			log.Printf("MetricTemplateReconciler: Found matching (for delete from Canary) entry for metricTemplate: NameTpl: %s, Namespace: %s", info.NameTpl, info.Namespace)

			// объектоы Canary в том же неймспейсе, где был удален metricTemplate
			canaries := &flaggerv1beta1.CanaryList{}
			listOpts := &client.ListOptions{Namespace: namespace}

			if err := r.Client.List(ctx, canaries, listOpts); err != nil {
				return ctrl.Result{}, err
			}

			// удаление соответствующих элементов из Spec.Analysis.Metrics
			for _, canary := range canaries.Items {
				if canary.Spec.Analysis != nil && canary.Spec.Analysis.Metrics != nil {
					updatedMetrics := []flaggerv1beta1.CanaryMetric{}

					for _, metric := range canary.Spec.Analysis.Metrics {
						if metric.TemplateRef != nil && metric.TemplateRef.Name == name {
							// пропуск того что не нужно удалять
							continue
						}

						// Этот элемент остается в списке
						updatedMetrics = append(updatedMetrics, metric)
					}

					// Обновление Spec.Analysis.Metrics в объекте Canary
					canary.Spec.Analysis.Metrics = updatedMetrics
					log.Printf("MetricTemplateReconciler: Delete from Canary, entry for metricTemplate: NameTpl: %s, Namespace: %s", info.NameTpl, info.Namespace)
					// Обновление самого объекта Canary
					if err := r.Client.Update(ctx, &canary); err != nil {
						return ctrl.Result{}, err
					}
					log.Printf("MetricTemplateReconciler: Finish deleteting entry from Canary resource")
				}
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *MetricTemplateReconciler) handleCreationOrUpdate(ctx context.Context, metricTemplate *flaggerv1beta1.MetricTemplate) (ctrl.Result, error) {
	// логика для создания или обновления объекта metricTemplate
	// ключ (client.ObjectKey) для объекта flaggerv1.Template, используя namespace и имя
	templateKey := client.ObjectKey{
		Namespace: metricTemplate.Namespace,
		Name:      "basic", // фильтр для определения Базовых метрик темплейтов
	}
	log.Print("MetricTemplateReconciler: Detect Update or Create request for flagger metricTemplate: ", metricTemplate.Name, ", in namespace: ", metricTemplate.Namespace)
	template := &flaggerv1.Template{}

	// Get для получения объекта Template
	if err := r.Client.Get(ctx, templateKey, template); err != nil {
		return ctrl.Result{}, err
	} else {

		// Проверяем, что имя объекта равно "basic, как темплейт для базовых метрик"
		if template.Name != "basic" {
			return ctrl.Result{}, nil
		}

		for _, namespaceSpec := range template.Spec.Namespaces {
			if namespaceSpec.Name != metricTemplate.Namespace {
				// Пропускать неймспейсы, которые не соответствуют неймспейсу сработавшего события
				continue
			}

			// Получаем имя метриктемплейта из metricTemplate
			metricTemplateName := metricTemplate.Name
			var hasBaseLabel bool

			if metricTemplate.Labels != nil && metricTemplate.Labels["base"] == "true" {
				log.Print("MetricTemplateReconciler: Modified metricTemplate has BASE label, updating Canary resources")
				hasBaseLabel = true

			}

			if hasBaseLabel {
				// Получаем данные (Interval и ThresholdMin) для данного метриктемплейта из templates.flagger.3rd.io basic
				var interval string
				var thresholdMin float64
				for _, mtInfo := range namespaceSpec.MetricTemplates {
					if mtInfo.Name == metricTemplateName {
						log.Print("MetricTemplateReconciler: match found for triggeres MetricTemplate with templates.flagger.3rd.io basic, is the metric name: ", metricTemplateName, " namespace: ", metricTemplate.Namespace)
						interval = mtInfo.Interval
						thresholdMin = mtInfo.ThresholdRange.Max
						break
					}
				}

				if interval == "" {
					log.Print("MetricTemplateReconciler: You need add description for MetricTemplate name: ", metricTemplateName, " to templates.flagger.3rd.io basic, now this base metric template is available, but not described (interval and threshhold range), skip update in Canary")
					continue
				}

				// Получаем список всех Canary в текущем НС
				listOpts := &client.ListOptions{Namespace: metricTemplate.Namespace}
				if err := r.Client.List(ctx, &canaries, listOpts); err != nil {
					return ctrl.Result{}, err
				}

				// Обходим все Canary в текущем НС
				for _, canary := range canaries.Items {
					if canary.Spec.Analysis == nil {
						canary.Spec.Analysis = &flaggerv1beta1.CanaryAnalysis{}
					}
					if canary.Spec.Analysis.Metrics == nil {
						canary.Spec.Analysis.Metrics = []flaggerv1beta1.CanaryMetric{}
					}

					needUpdate := false
					found := false

					// Ищем соответствующий элемент в Canary metrics
					for i, metric := range canary.Spec.Analysis.Metrics {
						if metric.Name == metricTemplateName {
							found = true
							// Проверяем, что metric.Interval и thresholdMin совпадают с данными из analysis
							if metric.Interval != interval || (metric.ThresholdRange.Max != nil && *metric.ThresholdRange.Max != thresholdMin) {
								canary.Spec.Analysis.Metrics[i].Interval = interval
								canary.Spec.Analysis.Metrics[i].ThresholdRange.Max = &thresholdMin
								needUpdate = true
							}
							break
						}
					}

					if !found {
						needUpdate = true
						newMetric := flaggerv1beta1.CanaryMetric{
							Name:     metricTemplateName,
							Interval: interval,
							ThresholdRange: &flaggerv1beta1.CanaryThresholdRange{
								Max: &thresholdMin,
							},
							TemplateRef: &flaggerv1beta1.CrossNamespaceObjectReference{
								Name: metricTemplateName,
							},
						}
						canary.Spec.Analysis.Metrics = append(canary.Spec.Analysis.Metrics, newMetric)
					}

					if needUpdate {
						// Применяем обновления только когда необходимо
						log.Print("MetricTemplateReconciler: Need update for Canary ", canary.Name, " (", metricTemplateName, ") ", "start updating...")
						if err := r.Client.Update(ctx, &canary); err != nil {
							return ctrl.Result{}, err
						}
						log.Print("MetricTemplateReconciler: Update for Canary ", canary.Name, " (", metricTemplateName, ") ", "finished...")
					} else {
						log.Print("MetricTemplateReconciler: No need update for Canary ", canary.Name, " (", metricTemplateName, ") ", " skip updating...")
					}
				}
			} else {
				log.Print("MetricTemplateReconciler: Updated MetricTemplate ", metricTemplateName, "in namespace: ", metricTemplate.Namespace, " NOT base metric template, skipping update Canary Analisys...")
			}
		}
	}

	// ...
	return ctrl.Result{}, nil
}

func (r *TemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	template := &flaggerv1.Template{}
	log.Print("TemplateReconciler: Detect Modify request for templates.flagger.3rd.io :", req.NamespacedName.Name, ", in namespace: ", req.NamespacedName.Namespace)
	// Проверяем наличие объекта Template
	if err := r.Client.Get(ctx, req.NamespacedName, template); err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		// Если объект Template не найден останавливаемся
		return ctrl.Result{}, nil
	} else {

		// Проверяем, что имя объекта равно "basic, как темплейт для базовых метрик"
		if template.Name != "basic" {
			// Если имя не равно "basic", сворачиваемся
			return ctrl.Result{}, nil
		}
		analysisData = nil
		for _, namespaceSpec := range template.Spec.Namespaces {
			analysis := AnalysisBasic{
				Namespace:          namespaceSpec.Name,
				MetricTemplateName: make([]MetricTemplateInfo, len(namespaceSpec.MetricTemplates)),
			}

			for i, metricTemplate := range namespaceSpec.MetricTemplates {
				analysis.MetricTemplateName[i] = MetricTemplateInfo{
					TemplateName: metricTemplate.Name,
					Interval:     metricTemplate.Interval,
					ThresholdMin: metricTemplate.ThresholdRange.Max,
				}
			}

			analysisData = append(analysisData, analysis)
		}

		for _, analysis := range analysisData {
			// Устанавливаем параметры для поиска в конкретном пространстве имен
			listOpts := &client.ListOptions{Namespace: analysis.Namespace}

			// Получаем все объекты MetricTemplate в данном пространстве имен
			if err := r.Client.List(ctx, &metricTemplates, listOpts); err != nil {
				return ctrl.Result{}, err
			}

			// Получили список всех MetricTemplate в текущем НС
			// Фильтруем по лейблу base: true
			// Создаем новый список для фильтрованных MetricTemplate
			filteredMetricTemplates := []flaggerv1beta1.MetricTemplate{}

			// Фильтруем MetricTemplate и добавляем только те, у которых метка "base" равна "true"
			for _, mt := range metricTemplates.Items {
				if mt.Labels["base"] == "true" {
					filteredMetricTemplates = append(filteredMetricTemplates, mt)
				}
			}
			for _, mt := range filteredMetricTemplates {
				// Устанавливаем параметры для поиска в конкретном НС
				listOpts := &client.ListOptions{Namespace: analysis.Namespace}

				// Получаем все объекты Canary в НС
				if err := r.Client.List(ctx, &canaries, listOpts); err != nil {
					return ctrl.Result{}, err
				}

				// Получили список всех Canary в текущем НС.
				// canaries.Items для обработки каждого объекта по отдельности.
				for _, canary := range canaries.Items {
					needUpdate := false
					// Проверяем наличие секции analysis и metrics
					if canary.Spec.Analysis == nil {
						canary.Spec.Analysis = &flaggerv1beta1.CanaryAnalysis{}
					}
					if canary.Spec.Analysis.Metrics == nil {
						canary.Spec.Analysis.Metrics = []flaggerv1beta1.CanaryMetric{}
					}
					for _, analysisMetric := range analysis.MetricTemplateName {
						// Создаем ключ для проверки что метрик темплейт из Template существует в кластере как Флаггер Метрик Темплейт
						metricTemplateKey := client.ObjectKey{
							Namespace: analysis.Namespace,
							Name:      analysisMetric.TemplateName,
						}
						metricTemplate := &flaggerv1beta1.MetricTemplate{}
						if err := r.Client.Get(ctx, metricTemplateKey, metricTemplate); err != nil {
							if errors.IsNotFound(err) {
								// Метриктемплейт не существует в кластере
								log.Print("MetricTemplateReconciler: MetricTemplate from Template, not found as Flagger MetricTemplate object: ", analysisMetric.TemplateName, " skip add to Canarys...")
								continue // следующий цикл
							}
							return ctrl.Result{}, err
						}
						needUpdate = false
						found := false
						// Ищем соответствующий элемент в Canary metrics
						for i, metric := range canary.Spec.Analysis.Metrics {
							if metric.Name == analysisMetric.TemplateName {
								found = true
								// Проверяем, что metric.Interval не nil и сравниваем значение
								if metric.Interval != analysisMetric.Interval || (metric.ThresholdRange.Max != nil && *metric.ThresholdRange.Max != analysisMetric.ThresholdMin) {
									canary.Spec.Analysis.Metrics[i].Interval = analysisMetric.Interval
									canary.Spec.Analysis.Metrics[i].ThresholdRange.Max = &analysisMetric.ThresholdMin
									needUpdate = true
								}
								//needUpdate = false
								break
							}
						}

						if !found {
							needUpdate = true
							newMetric := flaggerv1beta1.CanaryMetric{
								Name:     analysisMetric.TemplateName,
								Interval: analysisMetric.Interval,
								ThresholdRange: &flaggerv1beta1.CanaryThresholdRange{
									Max: &analysisMetric.ThresholdMin,
								},
								TemplateRef: &flaggerv1beta1.CrossNamespaceObjectReference{
									Name: analysisMetric.TemplateName,
								},
							}
							canary.Spec.Analysis.Metrics = append(canary.Spec.Analysis.Metrics, newMetric)
						}
					}
					if needUpdate {
						log.Print("TemplateReconciler: Need update for Canary ", canary.Name, " (", mt.Name, "), start updating...")
						// Применяем обновления только если есть изменения
						if err := r.Client.Update(ctx, &canary); err != nil {
							return ctrl.Result{}, err
						}
						log.Print("TemplateReconciler: Update for Canary ", canary.Name, " (", mt.Name, ") ", "finished...")
					} else {
						log.Print("TemplateReconciler: No need update for Canary ", canary.Name, " (", mt.Name, "), skip updating...")
					}
				}
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *CanaryReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log.Print("CanaryReconciler: Detect Modify request for Canary : ", req.NamespacedName.Name, ", in namespace: ", req.NamespacedName.Namespace, ", sleep 10 seconds...")
	time.Sleep(10 * time.Second)
	// Получить объект Canary по запросу
	canary := &flaggerv1beta1.Canary{}
	if err := r.Get(ctx, req.NamespacedName, canary); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	templateKey := client.ObjectKey{
		Namespace: canary.Namespace,
		Name:      "basic",
	}

	// Проверить, что событие не связано с удалением Canary
	if canary.ObjectMeta.DeletionTimestamp == nil {
		log.Print("CanaryReconciler: Canary " + canary.Name + " in Namespace " + canary.Namespace + " modified, now check base MetricTemplate is correct...")
		template := &flaggerv1.Template{}

		// Get для получения объекта Template
		if err := r.Client.Get(ctx, templateKey, template); err != nil {
			return ctrl.Result{}, err
		} else {

			// Проверяем, что имя объекта равно "basic, как темплейт для базовых метрик"
			if template.Name != "basic" {
				// Если имя не равно "basic", сворачиваемся
				return ctrl.Result{}, nil
			}
			var analysisData []AnalysisBasic

			for _, namespaceSpec := range template.Spec.Namespaces {
				if namespaceSpec.Name == req.NamespacedName.Namespace {
					analysis := AnalysisBasic{
						Namespace:          namespaceSpec.Name,
						MetricTemplateName: make([]MetricTemplateInfo, len(namespaceSpec.MetricTemplates)),
					}

					for i, metricTemplate := range namespaceSpec.MetricTemplates {
						analysis.MetricTemplateName[i] = MetricTemplateInfo{
							TemplateName: metricTemplate.Name,
							Interval:     metricTemplate.Interval,
							ThresholdMin: metricTemplate.ThresholdRange.Max,
						}
					}

					analysisData = append(analysisData, analysis)
					break // заполняем только для НС где произошло событие и прерываем заполнение
				}
			}

			for _, analysis := range analysisData {
				// Устанавливаем параметры для поиска в конкретном пространстве имен
				listOpts := &client.ListOptions{Namespace: analysis.Namespace}

				// Получаем все объекты MetricTemplate в данном пространстве имен
				if err := r.Client.List(ctx, &metricTemplates, listOpts); err != nil {
					return ctrl.Result{}, err
				}

				// Получили список всех MetricTemplate в текущем НС
				// Фильтруем по лейблу base: true
				// Создаем новый список для фильтрованных MetricTemplate
				filteredMetricTemplates := []flaggerv1beta1.MetricTemplate{}

				// Фильтруем MetricTemplate и добавляем только те, у которых метка "base" равна "true"
				for _, mt := range metricTemplates.Items {
					if mt.Labels["base"] == "true" {
						filteredMetricTemplates = append(filteredMetricTemplates, mt)
					}
				}

				// Выполнить логику для всех найденных MetricTemplate
				for _, mt := range filteredMetricTemplates {
					// проверяем что есть чем золнить данные из templates.flagger.3rd.io для этого метрикТемплейта
					for _, mtInfo := range analysis.MetricTemplateName {
						if mt.Name == mtInfo.TemplateName {
							needUpdate := false
							found := false
							// Ищем соответствующий элемент в Canary metrics
							for i, metric := range canary.Spec.Analysis.Metrics {
								if metric.Name == mt.Name {
									found = true
									// Проверяем, что metric.Interval не nil и сравниваем значение
									if metric.Interval != mtInfo.Interval || (metric.ThresholdRange.Max != nil && *metric.ThresholdRange.Max != mtInfo.ThresholdMin) {
										canary.Spec.Analysis.Metrics[i].Interval = mtInfo.Interval
										canary.Spec.Analysis.Metrics[i].ThresholdRange.Max = &mtInfo.ThresholdMin
										needUpdate = true
									}
									break
								}
							}

							if !found {
								needUpdate = true
								newMetric := flaggerv1beta1.CanaryMetric{
									Name:     mt.Name,
									Interval: mtInfo.Interval,
									ThresholdRange: &flaggerv1beta1.CanaryThresholdRange{
										Max: &mtInfo.ThresholdMin,
									},
									TemplateRef: &flaggerv1beta1.CrossNamespaceObjectReference{
										Name: mt.Name,
									},
								}
								canary.Spec.Analysis.Metrics = append(canary.Spec.Analysis.Metrics, newMetric)
							}

							// Применяем обновления если есть изменения
							if needUpdate {
								log.Print("CanaryReconciler: Need update for Canary ", canary.Name, " (", mt.Name, ") ", "start updating...")
								if err := r.Client.Update(ctx, canary); err != nil {
									return ctrl.Result{}, err
								}
								log.Print("CanaryReconciler: Update for Canary ", canary.Name, " (", mt.Name, ") ", "finished...")
							} else {
								log.Print("CanaryReconciler: No need update for Canary ", canary.Name, " (", mt.Name, "), skip updating...")
							}
						}
					}
				}
			}
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	templateController := ctrl.NewControllerManagedBy(mgr).
		For(&flaggerv1.Template{}).
		Complete(r)

	metricTemplateController := ctrl.NewControllerManagedBy(mgr).
		For(&flaggerv1beta1.MetricTemplate{}).
		Complete(&MetricTemplateReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			Log:    ctrl.Log.WithName("controllers").WithName("MetricTemplate"),
		})

	canaryController := ctrl.NewControllerManagedBy(mgr).
		For(&flaggerv1beta1.Canary{}).
		Complete(&CanaryReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			Log:    ctrl.Log.WithName("controllers").WithName("Canary"),
		})

	if err := templateController; err != nil {
		return err
	}

	if err := metricTemplateController; err != nil {
		return err
	}

	if err := canaryController; err != nil {
		return err
	}

	return nil
}
