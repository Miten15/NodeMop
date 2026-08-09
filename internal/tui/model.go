package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Miten15/NodeMop/internal/ai"
	"github.com/Miten15/NodeMop/internal/cleanup"
	"github.com/Miten15/NodeMop/internal/gitx"
	"github.com/Miten15/NodeMop/internal/project"
	"github.com/Miten15