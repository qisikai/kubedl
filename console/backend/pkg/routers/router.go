package routers

import (
	"flag"
	md "github.com/alibaba/kubedl/console/backend/pkg/middleware"
	"github.com/alibaba/kubedl/console/backend/pkg/routers/api"
	"github.com/alibaba/kubedl/console/backend/pkg/utils"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/alibaba/kubedl/console/backend/pkg/auth"
	"github.com/alibaba/kubedl/console/backend/pkg/constants"
	"github.com/alibaba/kubedl/console/backend/pkg/handlers"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"

	"k8s.io/klog"
)

func init() {
	flag.StringVar(&eventStorage, "event-storage", "apiserver", "event storage backend plugin name, persist events into backend if it's specified")
}

var (
	//eventStorage storage backend plugin name, persist events into backend
	eventStorage string
	//objectStorage storage backend plugin name, persist jobs and pods into backend
	objectStorage = "proxy"
)

type APIController interface {
	RegisterRoutes(routes *gin.RouterGroup)
}

//InitRouter load handlers and middlewares, it contains check auth, frontend resource,
//	error recovery etc. and returns an instance of Engine
func InitRouter() *gin.Engine {
	r := gin.New()
	//load basic middlewares
	r.Use(
		gin.Logger(),
		gin.Recovery(),
		func(context *gin.Context) {
			context.Header("Cache-Control", "no-store,no-cache")
		},
	)

	//register error requests routers
	r.NoRoute(
		utils.Redirect500,
		utils.Redirect403,
		utils.Redirect404,
	)

	//load session middleware
	store := cookie.NewStore([]byte("secret"))
	store.Options(sessions.Options{
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions("loginSession", store))

	//register frontend pages and static resource routers
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	distDir := path.Join(wd, "dist")
	klog.Infof("kubedl-console working dir: %s", wd)

	r.LoadHTMLFiles(distDir + "/index.html")
	r.Use(
		static.Serve("/", static.LocalFile(distDir, true)),
		func(context *gin.Context) {
			if context.Request.URL == nil || context.Request.URL.Path == "" ||
				!strings.HasPrefix(context.Request.URL.Path, constants.ApiV1Routes) {
				context.HTML(http.StatusOK, "index.html", gin.H{})
			}
		},
	)

	// Register api v1 customized routers.
	if err = initControllersRoute(r, constants.ApiV1Routes); err != nil {
		klog.Errorf("init controller routes error: %v", err)
		panic(err)
	}

	return r
}

//initControllersRoute registers customized controllers and adds routes
func initControllersRoute(r *gin.Engine, baseGroup string) error {
	//load check auth middleware
	authHandler := auth.GetAuth()
	r.Use(md.CheckAuthMiddleware(authHandler))

	logHandler, err := handlers.NewLogHandler(eventStorage)
	if err != nil {
		return err
	}
	jobHandler, err := handlers.NewJobHandler(objectStorage, logHandler)
	if err != nil {
		return err
	}
	//register controllers
	ctrls := []APIController{
		api.NewJobAPIsController(jobHandler),
		api.NewAuthAPIsController(authHandler),
		api.NewLogsAPIsController(logHandler),
		api.NewKubeDLAPIsController(),
		api.NewTensorBoardController(),
		api.NewDataAPIsController(),
		api.NewDataSourceAPIsController(),
		api.NewCodeSourceAPIsController(),
	}
	//register routes
	router := r.Group(baseGroup)
	for _, ctrl := range ctrls {
		ctrl.RegisterRoutes(router)
	}
	return nil
}
