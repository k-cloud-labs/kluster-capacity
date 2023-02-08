package options

import (
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NamespaceNames []*NamespaceName

type NamespaceName struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func (n NamespaceNames) Set(nns string) error {
	for _, nn := range strings.Split(nns, ",") {
		nnStrs := strings.Split(nn, "/")
		if len(nnStrs) == 1 {
			n = append(n, &NamespaceName{
				Namespace: metav1.NamespaceDefault,
				Name:      nnStrs[0],
			})
		} else if len(nnStrs) == 2 {
			n = append(n, &NamespaceName{
				Namespace: nnStrs[0],
				Name:      nnStrs[1],
			})
		} else {
			return errors.New("invalid format")
		}
	}

	return nil
}

func (n NamespaceNames) String() string {
	strs := []string{}
	for _, nn := range n {
		strs = append(strs, fmt.Sprintf("%s/%s", nn.Namespace, nn.Name))
	}

	return strings.Join(strs, ",")
}

func (n NamespaceNames) Type() string {
	return "NamespaceNames"
}
