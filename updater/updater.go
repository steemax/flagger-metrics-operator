package updater

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type MetricTemplateInfo struct {
	NameTpl   string // Имя MetricTemplate
	Namespace string // Неймспейс
	LabelHave bool   // Флаг наличия лейбла "base: true"
}

// Создайте слайс для хранения данных о MetricTemplate с лейблом "base: true"
var MetricTemplateInfoList []MetricTemplateInfo

func UpdateInfo() {
	// Настройте конфигурацию Kubernetes клиента
	config, err := rest.InClusterConfig() // Используйте InClusterConfig для кластера внутри Kubernetes
	if err != nil {
		fmt.Printf("Updater: Failed to create in-cluster config: %v\n", err)
		return
	}

	// Создайте клиент dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		fmt.Printf("Updater: Failed to create dynamic client: %v\n", err)
		return
	}

	// Создайте клиент Kubernetes
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("Updater: Failed to create Kubernetes client: %v\n", err)
		return
	}

	// Получите список всех неймспейсов
	namespaceList, err := kubeClient.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		fmt.Printf("Updater: Failed to list namespaces: %v\n", err)
		return
	}

	// Настройте параметры запроса
	gvr := schema.GroupVersionResource{
		Group:    "flagger.app",
		Version:  "v1beta1",
		Resource: "metrictemplates",
	}

	// Пройдитесь по каждому неймспейсу
	for _, ns := range namespaceList.Items {
		namespace := ns.Name

		// Создайте контекст с таймаутом
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Получите список объектов MetricTemplate в текущем неймспейсе
		unstructuredList, err := dynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Printf("Failed to list MetricTemplates in namespace %s: %v\n", namespace, err)
			continue
		}

		// Итерируйтесь по объектам MetricTemplate и добавляйте информацию в структуру
		for _, obj := range unstructuredList.Items {
			labels := obj.GetLabels()
			if labels != nil && labels["base"] == "true" {
				metricTemplateInfo := MetricTemplateInfo{
					NameTpl:   obj.GetName(),
					Namespace: namespace,
					LabelHave: true,
				}

				// Обнулите структуру перед добавлением
				MetricTemplateInfoList = append(MetricTemplateInfoList, metricTemplateInfo)
			}
		}
	}

}
func UpdateInfoPeriodically() {
	// Бесконечный цикл для обновления информации каждую минуту
	for {
		MetricTemplateInfoList = nil // Обнуляем metricTemplateInfo перед записью данных

		UpdateInfo() // Вызываем функцию обновления информации
		log.Println("Updater: Update cache information about MetricTemplates in cluster scope:")
		for _, info := range MetricTemplateInfoList {
			log.Printf("Updater: NameTpl: %s, Namespace: %s, LabelHave: %v\n", info.NameTpl, info.Namespace, info.LabelHave)
		}
		os.Stdout.Sync() // Принудительная синхронизация вывода
		// Пауза на 1 минуту перед следующим вызовом
		time.Sleep(3 * time.Minute)
	}
}
